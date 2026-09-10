package api

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"wayback/internal/config"
)

// sessionCookie 从响应中取出会话 Cookie，未找到时返回 nil。
func sessionCookie(w *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == SessionCookieName {
			return c
		}
	}
	return nil
}

func TestSession_BasicAuthIssuesCookie(t *testing.T) {
	r := setupAuthRouter(&config.AuthConfig{Password: "secret"}, &config.ServerConfig{})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/", nil)
	req.SetBasicAuth("wayback", "secret")
	r.ServeHTTP(w, req)

	cookie := sessionCookie(w)
	if cookie == nil {
		t.Fatal("expected session cookie after successful Basic Auth")
	}
	if !cookie.HttpOnly {
		t.Error("session cookie must be HttpOnly")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("expected SameSite=Lax, got %v", cookie.SameSite)
	}
	if cookie.Path != "/" {
		t.Errorf("expected Path=/, got %q", cookie.Path)
	}
	// MaxAge 必须为正，否则 Cookie 是会话级的，浏览器重启即丢失,
	// 那就完全没有解决原本的问题。
	if cookie.MaxAge <= 0 {
		t.Errorf("expected persistent cookie with positive MaxAge, got %d", cookie.MaxAge)
	}
}

func TestSession_FailedBasicAuthIssuesNoCookie(t *testing.T) {
	r := setupAuthRouter(&config.AuthConfig{Password: "secret"}, &config.ServerConfig{})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/", nil)
	req.SetBasicAuth("wayback", "wrong")
	r.ServeHTTP(w, req)

	if cookie := sessionCookie(w); cookie != nil {
		t.Error("expected no session cookie when Basic Auth fails")
	}
}

func TestSession_CookieGrantsAccessWithoutBasicAuth(t *testing.T) {
	r := setupAuthRouter(&config.AuthConfig{Password: "secret"}, &config.ServerConfig{})

	// 第一步：用 Basic Auth 换取 Cookie
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/", nil)
	req.SetBasicAuth("wayback", "secret")
	r.ServeHTTP(w, req)

	cookie := sessionCookie(w)
	if cookie == nil {
		t.Fatal("setup failed: no session cookie issued")
	}

	// 第二步：只带 Cookie，模拟浏览器重启后的请求
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("GET", "/", nil)
	req2.AddCookie(&http.Cookie{Name: SessionCookieName, Value: cookie.Value})
	r.ServeHTTP(w2, req2)

	if w2.Code == http.StatusUnauthorized {
		t.Error("expected session cookie alone to grant access, got 401")
	}
}

func TestSession_TamperedCookieRejectedWithChallenge(t *testing.T) {
	r := setupAuthRouter(&config.AuthConfig{Password: "secret"}, &config.ServerConfig{})

	// 伪造一个远期过期时间，但签名是垃圾数据
	forged := strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10) + ".deadbeef"

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: forged})
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for tampered cookie, got %d", w.Code)
	}
	// 必须重新发起挑战，否则用户会卡在一个无法自救的错误页上。
	if !strings.HasPrefix(w.Header().Get("WWW-Authenticate"), "Basic ") {
		t.Error("expected WWW-Authenticate challenge so the native prompt reappears")
	}
}

func TestSession_ExpiredCookieRejectedWithChallenge(t *testing.T) {
	r := setupAuthRouter(&config.AuthConfig{Password: "secret"}, &config.ServerConfig{})

	// 签名合法，但过期时间在过去
	expired := signSession("secret", time.Now().Add(-time.Minute))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: expired})
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for expired cookie, got %d", w.Code)
	}
	if w.Header().Get("WWW-Authenticate") == "" {
		t.Error("expected WWW-Authenticate challenge for expired cookie")
	}
}

func TestSession_CookieFromDifferentPasswordRejected(t *testing.T) {
	// 改密码必须使旧会话立即失效。
	stale := signSession("old-password", time.Now().Add(sessionTTL))

	r := setupAuthRouter(&config.AuthConfig{Password: "new-password"}, &config.ServerConfig{})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: stale})
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for cookie signed with old password, got %d", w.Code)
	}
}

func TestVerifySession_RejectsMalformedTokens(t *testing.T) {
	valid := signSession("secret", time.Now().Add(time.Hour))

	cases := []struct {
		name  string
		token string
	}{
		{"empty", ""},
		{"no separator", "abcdef"},
		{"empty payload", ".c2ln"},
		{"empty signature", "1900000000."},
		{"non-numeric payload", "notanumber.c2ln"},
		{"signature only", strings.SplitN(valid, ".", 2)[1]},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if verifySession("secret", tc.token) {
				t.Errorf("expected %q to be rejected", tc.token)
			}
		})
	}
}

func TestVerifySession_EmptyPasswordRejectsEverything(t *testing.T) {
	// 认证关闭时中间件不挂载，但 verifySession 本身也不该给空密码放行。
	if verifySession("", signSession("", time.Now().Add(time.Hour))) {
		t.Error("expected empty password to reject all tokens")
	}
}

func TestSession_SecureFlagFollowsScheme(t *testing.T) {
	r := setupAuthRouter(&config.AuthConfig{Password: "secret"}, &config.ServerConfig{})

	// 纯 HTTP：不能加 Secure，否则本地 http://localhost 部署下
	// 浏览器会直接丢弃 Cookie。
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/", nil)
	req.SetBasicAuth("wayback", "secret")
	r.ServeHTTP(w, req)

	if cookie := sessionCookie(w); cookie == nil || cookie.Secure {
		t.Error("expected non-Secure cookie over plain HTTP")
	}

	// 反向代理终止 TLS 的场景
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("GET", "/", nil)
	req2.SetBasicAuth("wayback", "secret")
	req2.Header.Set("X-Forwarded-Proto", "https")
	r.ServeHTTP(w2, req2)

	if cookie := sessionCookie(w2); cookie == nil || !cookie.Secure {
		t.Error("expected Secure cookie when X-Forwarded-Proto is https")
	}
}

