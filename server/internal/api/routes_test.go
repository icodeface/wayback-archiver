package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"wayback/internal/config"
)

func setupAuthRouter(authCfg *config.AuthConfig, serverCfg *config.ServerConfig) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	handler := &Handler{} // minimal handler, routes not hit

	SetupRoutes(r, handler, authCfg, serverCfg, "test", "")
	return r
}

func TestRoutes_NoAuth_AllowsAccess(t *testing.T) {
	r := setupAuthRouter(&config.AuthConfig{Password: ""}, &config.ServerConfig{})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/", nil)
	r.ServeHTTP(w, req)

	// Should not be 401
	if w.Code == http.StatusUnauthorized {
		t.Errorf("expected no auth challenge, got 401")
	}
}

func TestRoutes_WithAuth_RejectsNoCredentials(t *testing.T) {
	r := setupAuthRouter(&config.AuthConfig{Password: "secret"}, &config.ServerConfig{})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without credentials, got %d", w.Code)
	}
}

func TestRoutes_WithAuth_AcceptsValidCredentials(t *testing.T) {
	r := setupAuthRouter(&config.AuthConfig{Password: "secret"}, &config.ServerConfig{})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/", nil)
	req.SetBasicAuth("wayback", "secret")
	r.ServeHTTP(w, req)

	if w.Code == http.StatusUnauthorized {
		t.Errorf("expected access with valid credentials, got 401")
	}
}

func TestRoutes_WithAuth_RejectsWrongPassword(t *testing.T) {
	r := setupAuthRouter(&config.AuthConfig{Password: "secret"}, &config.ServerConfig{})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/", nil)
	req.SetBasicAuth("wayback", "wrong")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 with wrong password, got %d", w.Code)
	}
}

func TestRoutes_WithAuth_RejectsWrongUsername(t *testing.T) {
	r := setupAuthRouter(&config.AuthConfig{Password: "secret"}, &config.ServerConfig{})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/", nil)
	req.SetBasicAuth("admin", "secret")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 with wrong username, got %d", w.Code)
	}
}

func TestRoutes_CORS_IncludesAuthorizationHeader(t *testing.T) {
	r := setupAuthRouter(&config.AuthConfig{Password: ""}, &config.ServerConfig{})

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

func TestRoutes_DebugAPI_DisabledByDefault(t *testing.T) {
	r := setupAuthRouter(&config.AuthConfig{Password: ""}, &config.ServerConfig{})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/debug/memstats", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 when debug API is disabled, got %d", w.Code)
	}
}

func TestRoutes_DebugAPI_CanBeEnabled(t *testing.T) {
	r := setupAuthRouter(&config.AuthConfig{Password: ""}, &config.ServerConfig{EnableDebugAPI: true})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/debug/memstats", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 when debug API is enabled, got %d", w.Code)
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

func TestRoutes_HEAD_ProxyResource(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	// Register a simple handler that returns 200 for HEAD requests
	r.HEAD("/archive/:page_id/:timestamp/*resource_path", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("HEAD", "/archive/123/20240309150405mp_/https://example.com/style.css", nil)
	r.ServeHTTP(w, req)

	// Should not be 404 (route should exist)
	if w.Code == http.StatusNotFound {
		t.Errorf("HEAD /archive/:page_id/:timestamp/*resource_path route not registered, got 404")
	}
}

func TestRoutes_HEAD_ServeLocalResource(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	// Register a simple handler that returns 200 for HEAD requests
	r.HEAD("/archive/resources/*filepath", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("HEAD", "/archive/resources/ab/cd/hash.css", nil)
	r.ServeHTTP(w, req)

	// Should not be 404 (route should exist)
	if w.Code == http.StatusNotFound {
		t.Errorf("HEAD /archive/resources/*filepath route not registered, got 404")
	}
}
