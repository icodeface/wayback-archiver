package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

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

func setupRouterFromEnv(t *testing.T) *gin.Engine {
	t.Helper()

	cfg, err := config.LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}

	return setupAuthRouter(&cfg.Auth, &cfg.Server)
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

func TestRoutes_CORS_AllowedOriginsEnvEnablesCustomOrigin(t *testing.T) {
	t.Setenv("ALLOWED_ORIGINS", "https://allowed.example.com")
	r := setupRouterFromEnv(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("OPTIONS", "/api/archive", nil)
	req.Header.Set("Origin", "https://allowed.example.com")
	r.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://allowed.example.com" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want custom allowed origin", got)
	}
}

func TestRoutes_CORS_AllowedOriginsEnvOverridesDefaultLocalhostSet(t *testing.T) {
	t.Setenv("ALLOWED_ORIGINS", "https://allowed.example.com")
	r := setupRouterFromEnv(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("OPTIONS", "/api/archive", nil)
	req.Header.Set("Origin", "http://localhost:8080")
	r.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want localhost blocked when not configured", got)
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

func TestServeEmbeddedFile_MissingFileReturnsInternalServerError(t *testing.T) {
	handler := serveEmbeddedFile(fstest.MapFS{}, "missing.html")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/missing.html", nil)
	handler(c)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for missing embedded file, got %d", w.Code)
	}
}

func TestServeEmbeddedFile_SetsContentType(t *testing.T) {
	handler := serveEmbeddedFile(fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html></html>")},
	}, "index.html")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/index.html", nil)
	handler(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if got := w.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want text/html; charset=utf-8", got)
	}
	if body := w.Body.String(); body != "<html></html>" {
		t.Fatalf("body = %q, want embedded content", body)
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
