package api

import (
	"encoding/json"
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

func TestGetLogRange_ReturnsOffsets(t *testing.T) {
	handler, cleanup := setupLogTestHandler(t)
	defer cleanup()

	filename := "wayback-2099-04-15.001.log"
	content := "one\ntwo\nthree\nfour\n"
	path := filepath.Join(handler.logger.Dir(), filename)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	router := setupLogRouter(handler)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/logs/"+filename+"?limit=11", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body struct {
		Content       string `json:"content"`
		Filename      string `json:"filename"`
		StartOffset   int64  `json:"start_offset"`
		EndOffset     int64  `json:"end_offset"`
		FileSize      int64  `json:"file_size"`
		HasMoreBefore bool   `json:"has_more_before"`
		HasMoreAfter  bool   `json:"has_more_after"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if body.Filename != filename {
		t.Fatalf("unexpected filename: %s", body.Filename)
	}
	if body.Content != "three\nfour\n" {
		t.Fatalf("unexpected content: %q", body.Content)
	}
	if body.StartOffset != int64(len("one\ntwo\n")) || body.EndOffset != int64(len(content)) || body.FileSize != int64(len(content)) {
		t.Fatalf("unexpected offsets: start=%d end=%d size=%d", body.StartOffset, body.EndOffset, body.FileSize)
	}
	if !body.HasMoreBefore || body.HasMoreAfter {
		t.Fatalf("unexpected more flags: before=%v after=%v", body.HasMoreBefore, body.HasMoreAfter)
	}
}

func TestGetLogRange_InvalidBeforeReturnsBadRequest(t *testing.T) {
	handler, cleanup := setupLogTestHandler(t)
	defer cleanup()

	router := setupLogRouter(handler)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/logs/latest?before=-1&limit=1024", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid before, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetLogRange_BeforeAndAfterReturnsBadRequest(t *testing.T) {
	handler, cleanup := setupLogTestHandler(t)
	defer cleanup()

	router := setupLogRouter(handler)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/logs/latest?before=10&after=20&limit=1024", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for before and after, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetLatestLogRange_OffsetRequiresExplicitFilename(t *testing.T) {
	handler, cleanup := setupLogTestHandler(t)
	defer cleanup()

	router := setupLogRouter(handler)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/logs/latest?after=10&limit=1024", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for latest offset range, got %d: %s", w.Code, w.Body.String())
	}
}
