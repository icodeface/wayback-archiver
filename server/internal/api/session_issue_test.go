package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"wayback/internal/config"
)

func TestShouldIssueCookie_HTMLPagesAndAPIs(t *testing.T) {
	cases := []struct {
		path   string
		should bool
	}{
		// HTML 页面 - 应该下发
		{"/", true},
		{"/index.html", true},
		{"/favorites", true},
		{"/view/123", true},
		{"/view/123?foo=bar", true},

		// API 端点 - 应该下发
		{"/api/pages", true},
		{"/api/archive", true},
		{"/api/version", true},
		{"/api/search?q=test", true},

		// 归档资源 - 不应该下发
		{"/archive/resources/abc.css", false},
		{"/archive/resources/images/photo.jpg", false},
		{"/archive/123/1234567890/style.css", false},
		{"/archive/123/1234567890/script.js", false},

		// 分享资源 - 不应该下发（虽然已在认证前，但保持一致）
		{"/share/token123/archive/1234567890/image.png", false},
	}

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			got := shouldIssueCookie(tc.path)
			if got != tc.should {
				t.Errorf("shouldIssueCookie(%q) = %v, want %v", tc.path, got, tc.should)
			}
		})
	}
}

func TestAuthMiddleware_OnlyIssuesCookieForNonResourcePaths(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name         string
		path         string
		expectCookie bool
	}{
		{"HTML page", "/", true},
		{"API version", "/api/version", true},
		{"Archive resource", "/archive/resources/style.css", false},
		{"Archive timestamp resource", "/archive/123/1234567890/image.jpg", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			authCfg := &config.AuthConfig{Password: "secret"}

			// 只挂认证中间件和一个简单的 handler
			if authCfg.Enabled() {
				r.Use(AuthMiddleware(authCfg.Password))
			}
			r.GET("/*any", func(c *gin.Context) {
				c.String(http.StatusOK, "ok")
			})

			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", tt.path, nil)
			req.SetBasicAuth("wayback", "secret")
			r.ServeHTTP(w, req)

			cookie := sessionCookie(w)
			if tt.expectCookie && cookie == nil {
				t.Errorf("expected cookie to be issued for %s", tt.path)
			}
			if !tt.expectCookie && cookie != nil {
				t.Errorf("expected no cookie for %s, but got: %s", tt.path, cookie.Value)
			}
		})
	}
}

func TestAuthMiddleware_ResourceRequestWithCookieStillWorks(t *testing.T) {
	r := setupAuthRouter(&config.AuthConfig{Password: "secret"}, &config.ServerConfig{})

	// 第一步：从 HTML 页面获取 Cookie
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/", nil)
	req.SetBasicAuth("wayback", "secret")
	r.ServeHTTP(w, req)

	cookie := sessionCookie(w)
	if cookie == nil {
		t.Fatal("setup failed: no cookie issued for HTML page")
	}

	// 第二步：用 Cookie 访问资源路径（模拟浏览器加载子资源）
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("GET", "/archive/resources/style.css", nil)
	req2.AddCookie(&http.Cookie{Name: SessionCookieName, Value: cookie.Value})
	r.ServeHTTP(w2, req2)

	if w2.Code == http.StatusUnauthorized {
		t.Error("expected cookie to grant access to resource path, got 401")
	}
	// 资源请求不应该再下发新 Cookie
	if newCookie := sessionCookie(w2); newCookie != nil {
		t.Error("expected no new cookie issued for resource path")
	}
}
