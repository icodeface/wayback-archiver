package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"wayback/internal/logging"
)

func setupLogTestHandler(t *testing.T) (*Handler, func()) {
	t.Helper()

	logDir := t.TempDir()
	logger, err := logging.Setup(logDir)
	if err != nil {
		t.Fatalf("logging.Setup failed: %v", err)
	}

	handler := &Handler{logger: logger}
	cleanup := func() {
		logger.Close()
	}
	return handler, cleanup
}

func setupLogRouter(handler *Handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api")
	api.GET("/logs", handler.ListLogs)
	api.GET("/logs/latest", handler.GetLatestLog)
	api.GET("/logs/:filename", handler.GetLog)
	return r
}

func TestGetLog_InvalidTail(t *testing.T) {
	handler, cleanup := setupLogTestHandler(t)
	defer cleanup()

	router := setupLogRouter(handler)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/logs/latest?tail=not-a-number", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid tail, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetLog_InvalidFilenameReturnsBadRequest(t *testing.T) {
	handler, cleanup := setupLogTestHandler(t)
	defer cleanup()

	router := setupLogRouter(handler)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/logs/evil.log", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid filename, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetLog_SymlinkReturnsInternalServerError(t *testing.T) {
	handler, cleanup := setupLogTestHandler(t)
	defer cleanup()

	target := filepath.Join(handler.logger.Dir(), "target.log")
	if err := os.WriteFile(target, []byte("hello"), 0644); err != nil {
		t.Fatalf("WriteFile target failed: %v", err)
	}
	linkName := filepath.Join(handler.logger.Dir(), "wayback-2099-04-15.001.log")
	if err := os.Symlink(target, linkName); err != nil {
		t.Fatalf("Symlink failed: %v", err)
	}

	router := setupLogRouter(handler)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/logs/wayback-2099-04-15.001.log", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for symlink log file, got %d: %s", w.Code, w.Body.String())
	}
}
