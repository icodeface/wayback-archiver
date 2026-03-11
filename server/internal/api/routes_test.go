package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"wayback/internal/config"
)

func setupAuthRouter(authCfg *config.AuthConfig) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	handler := &Handler{} // minimal handler, routes not hit

	SetupRoutes(r, handler, authCfg)
	return r
}

func TestRoutes_NoAuth_AllowsAccess(t *testing.T) {
	r := setupAuthRouter(&config.AuthConfig{Password: ""})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/", nil)
	r.ServeHTTP(w, req)

	// Should not be 401
	if w.Code == http.StatusUnauthorized {
		t.Errorf("expected no auth challenge, got 401")
	}
}

func TestRoutes_WithAuth_RejectsNoCredentials(t *testing.T) {
	r := setupAuthRouter(&config.AuthConfig{Password: "secret"})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without credentials, got %d", w.Code)
	}
}

func TestRoutes_WithAuth_AcceptsValidCredentials(t *testing.T) {
	r := setupAuthRouter(&config.AuthConfig{Password: "secret"})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/", nil)
	req.SetBasicAuth("wayback", "secret")
	r.ServeHTTP(w, req)

	if w.Code == http.StatusUnauthorized {
		t.Errorf("expected access with valid credentials, got 401")
	}
}

func TestRoutes_WithAuth_RejectsWrongPassword(t *testing.T) {
	r := setupAuthRouter(&config.AuthConfig{Password: "secret"})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/", nil)
	req.SetBasicAuth("wayback", "wrong")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 with wrong password, got %d", w.Code)
	}
}

func TestRoutes_WithAuth_RejectsWrongUsername(t *testing.T) {
	r := setupAuthRouter(&config.AuthConfig{Password: "secret"})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/", nil)
	req.SetBasicAuth("admin", "secret")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 with wrong username, got %d", w.Code)
	}
}

func TestRoutes_CORS_IncludesAuthorizationHeader(t *testing.T) {
	r := setupAuthRouter(&config.AuthConfig{Password: ""})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("OPTIONS", "/api/archive", nil)
	r.ServeHTTP(w, req)

	allowHeaders := w.Header().Get("Access-Control-Allow-Headers")
	if allowHeaders == "" {
		t.Fatal("Access-Control-Allow-Headers not set")
	}
	if !containsSubstring(allowHeaders, "Authorization") {
		t.Errorf("CORS Allow-Headers = %q, want it to include Authorization", allowHeaders)
	}
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && stringContains(s, substr))
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
