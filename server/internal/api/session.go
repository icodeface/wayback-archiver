package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"wayback/internal/config"
)

const (
	// SessionCookieName 是会话 Cookie 的名称。
	SessionCookieName = "wayback_session"

	// sessionTTL 是会话有效期。浏览器重启后 Cookie 仍在磁盘上，
	// 因此这个时长决定了「多久需要再输一次密码」。
	sessionTTL = 365 * 24 * time.Hour

	// sessionKeyContext 用于派生签名密钥，避免密钥与其他用途混用。
	// 修改 AUTH_PASSWORD 会改变派生密钥，从而使所有已签发会话立即失效。
	sessionKeyContext = "wayback-session-v1|"
)

// sessionKey 从认证密码派生 HMAC 签名密钥。
func sessionKey(password string) []byte {
	sum := sha256.Sum256([]byte(sessionKeyContext + password))
	return sum[:]
}

// signSession 生成 "<expiryUnix>.<base64url(hmac)>" 形式的会话令牌。
func signSession(password string, expiry time.Time) string {
	payload := strconv.FormatInt(expiry.Unix(), 10)
	mac := hmac.New(sha256.New, sessionKey(password))
	mac.Write([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return payload + "." + sig
}

// verifySession 校验会话令牌的签名与有效期。
func verifySession(password, token string) bool {
	if password == "" || token == "" {
		return false
	}

	payload, sig, found := strings.Cut(token, ".")
	if !found || payload == "" || sig == "" {
		return false
	}

	expiryUnix, err := strconv.ParseInt(payload, 10, 64)
	if err != nil {
		return false
	}

	// 先做恒定时间的签名校验，再看过期时间，避免通过时序区分
	// 「签名无效」和「签名有效但已过期」。
	mac := hmac.New(sha256.New, sessionKey(password))
	mac.Write([]byte(payload))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(sig), []byte(expected)) != 1 {
		return false
	}

	return time.Now().Before(time.Unix(expiryUnix, 0))
}

// checkBasicAuth 以恒定时间比较 Basic Auth 凭据。
func checkBasicAuth(r *http.Request, password string) bool {
	user, pass, ok := r.BasicAuth()
	if !ok {
		return false
	}

	// 两个比较都要执行，不能短路，否则用户名错误会比密码错误更快返回。
	userOK := subtle.ConstantTimeCompare([]byte(user), []byte(config.AuthUsername))
	passOK := subtle.ConstantTimeCompare([]byte(pass), []byte(password))
	return userOK&passOK == 1
}

// requestIsHTTPS 判断请求是否经由 HTTPS 到达，用于决定是否给 Cookie 加 Secure。
// 反向代理场景下 TLS 在代理层终止，因此同时检查 X-Forwarded-Proto。
func requestIsHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	// 可能是 "https, http" 这样的链式取值，取第一段。
	proto, _, _ := strings.Cut(r.Header.Get("X-Forwarded-Proto"), ",")
	return strings.EqualFold(strings.TrimSpace(proto), "https")
}

// issueSessionCookie 在 Basic Auth 校验通过后下发长效会话 Cookie。
//
// SameSite=Lax：Cookie 不参与跨站请求。油猴脚本的上传是跨站的，
// 但它自带 Basic Auth 头，不依赖此 Cookie。不要改成 None 去「顺便简化脚本」，
// 那会打开 CSRF 面。
func issueSessionCookie(c *gin.Context, password string) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     SessionCookieName,
		Value:    signSession(password, time.Now().Add(sessionTTL)),
		Path:     "/",
		MaxAge:   int(sessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   requestIsHTTPS(c.Request),
		SameSite: http.SameSiteLaxMode,
	})
}

// AuthMiddleware 同时接受两种凭据：
//
//  1. 合法的会话 Cookie —— 浏览器重启后仍然有效，不再弹窗
//  2. 合法的 Basic Auth 头 —— 油猴脚本、curl、Puppeteer 无需改动
//
// Basic Auth 校验通过时顺带下发会话 Cookie，因此浏览器只需在原生弹窗里
// 输一次密码；之后的请求（含 /view/:id 的子资源）都靠 Cookie 放行。
//
// Cookie 无效与完全没有 Cookie 走同一条失败路径，都返回
// 401 + WWW-Authenticate，让原生弹窗重新出现，避免会话过期后
// 停在一个无法自救的错误页上。
func AuthMiddleware(password string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if cookie, err := c.Cookie(SessionCookieName); err == nil && verifySession(password, cookie) {
			c.Next()
			return
		}

		if checkBasicAuth(c.Request, password) {
			issueSessionCookie(c, password)
			c.Next()
			return
		}

		c.Header("WWW-Authenticate", `Basic realm="wayback", charset="UTF-8"`)
		c.AbortWithStatus(http.StatusUnauthorized)
	}
}
