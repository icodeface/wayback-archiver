package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"wayback/internal/config"
	"wayback/internal/database"
	"wayback/internal/models"
	"wayback/internal/storage"
)

func setupSQLiteShareTestHandler(t *testing.T) (*Handler, func()) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "wayback.db")
	db, err := database.NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("NewSQLite failed: %v", err)
	}

	dataDir := t.TempDir()
	fs := storage.NewFileStorage(dataDir)
	dedup := storage.NewDeduplicator(db, fs, config.ResourceConfig{
		Workers:         2,
		MetadataCacheMB: 16,
		DownloadTimeout: 30,
	})
	handler := NewHandler(dedup, db, dataDir, nil)
	cleanup := func() {
		dedup.WaitForBackgroundTasks()
		_ = db.Close()
	}
	return handler, cleanup
}

func setupShareRouter(handler *Handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/pages/:id/shares", handler.CreatePageShare)
	r.GET("/api/pages/:id/shares", handler.ListPageShares)
	r.DELETE("/api/shares/:id", handler.RevokePageShare)
	r.GET("/share/:token", handler.ViewSharedPage)
	r.GET("/share/:token/md", handler.ViewSharedPageMarkdown)
	r.GET("/share/:token/archive/:timestamp/*resource_path", handler.ProxySharedResource)
	r.GET("/share/:token/resources/*filepath", handler.ServeSharedLocalResource)
	return r
}

func writeShareTestFile(t *testing.T, baseDir, relPath, content string) {
	t.Helper()
	fullPath := filepath.Join(baseDir, relPath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
}

func createShareTestPage(t *testing.T, handler *Handler) int64 {
	t.Helper()

	htmlPath := "html/test/public-share.html"
	writeShareTestFile(t, handler.dataDir, htmlPath, `<html><body><h1>Shared Title</h1><link rel="stylesheet" href="/archive/1/20260410120000mp_/https://cdn.example.com/app.css"><img src="/archive/1/20260410120000mp_/https://cdn.example.com/logo.png"></body></html>`)
	writeShareTestFile(t, handler.dataDir, "resources/aa/bb/app.css", `.hero{background:url("../img/logo.png")}`)
	writeShareTestFile(t, handler.dataDir, "resources/cc/dd/logo.img", "shared-image")
	writeShareTestFile(t, handler.dataDir, "resources/ee/ff/private.img", "private-image")

	pageID, err := handler.db.CreatePage(
		"https://share-test.example.com/page",
		"Share Test",
		htmlPath,
		strings.Repeat("a", 64),
		time.Now().UTC(),
	)
	if err != nil {
		t.Fatalf("CreatePage failed: %v", err)
	}
	if err := handler.db.FinalizePageCreate(pageID, nil); err != nil {
		t.Fatalf("FinalizePageCreate failed: %v", err)
	}

	cssID, err := handler.db.CreateResource("https://cdn.example.com/app.css", strings.Repeat("b", 64), "css", "resources/aa/bb/app.css", 32)
	if err != nil {
		t.Fatalf("CreateResource css failed: %v", err)
	}
	imgID, err := handler.db.CreateResource("https://cdn.example.com/img/logo.png", strings.Repeat("c", 64), "image", "resources/cc/dd/logo.img", 12)
	if err != nil {
		t.Fatalf("CreateResource image failed: %v", err)
	}
	privateID, err := handler.db.CreateResource("https://cdn.example.com/private.png", strings.Repeat("d", 64), "image", "resources/ee/ff/private.img", 13)
	if err != nil {
		t.Fatalf("CreateResource private failed: %v", err)
	}
	if err := handler.db.LinkPageResource(pageID, cssID); err != nil {
		t.Fatalf("LinkPageResource css failed: %v", err)
	}
	if err := handler.db.LinkPageResource(pageID, imgID); err != nil {
		t.Fatalf("LinkPageResource image failed: %v", err)
	}
	_ = privateID

	return pageID
}

func createShareViaAPI(t *testing.T, router *gin.Engine, pageID int64) models.ShareResponse {
	t.Helper()

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/pages/"+strconv.FormatInt(pageID, 10)+"/shares", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("create share status = %d, body: %s", w.Code, w.Body.String())
	}
	var resp models.ShareResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Unmarshal share response failed: %v", err)
	}
	if resp.Token == "" || resp.SnapshotURL == "" || resp.MarkdownURL == "" {
		t.Fatalf("share response missing public URLs: %+v", resp)
	}
	return resp
}

func TestPublicShare_ViewMarkdownAndResources(t *testing.T) {
	handler, cleanup := setupSQLiteShareTestHandler(t)
	defer cleanup()

	pageID := createShareTestPage(t, handler)
	router := setupShareRouter(handler)
	share := createShareViaAPI(t, router, pageID)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, share.SnapshotURL, nil)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("shared page status = %d, body: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "PUBLIC SNAPSHOT") {
		t.Fatalf("shared page missing public header: %s", body)
	}
	if !strings.Contains(body, "/share/"+share.Token+"/archive/20260410120000mp_/https://cdn.example.com/app.css") {
		t.Fatalf("archive resource URL was not rewritten to share path: %s", body)
	}
	if strings.Contains(body, `href="/"`) || strings.Contains(body, "/timeline?") {
		t.Fatalf("public share should not expose private UI navigation: %s", body)
	}
	if got := w.Header().Get("X-Robots-Tag"); got != "noindex, nofollow" {
		t.Fatalf("X-Robots-Tag = %q", got)
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, share.MarkdownURL, nil)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("markdown status = %d, body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "# Shared Title") {
		t.Fatalf("unexpected markdown: %s", w.Body.String())
	}

	cssURL := "/share/" + share.Token + "/archive/20260410120000mp_/https://cdn.example.com/app.css"
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, cssURL, nil)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("css status = %d, body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `/share/`+share.Token+`/resources/cc/dd/logo.img`) {
		t.Fatalf("CSS subresource was not rewritten to share resource path: %s", w.Body.String())
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/share/"+share.Token+"/resources/cc/dd/logo.img", nil)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK || w.Body.String() != "shared-image" {
		t.Fatalf("shared local resource status = %d body = %q", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/share/"+share.Token+"/resources/ee/ff/private.img", nil)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("unshared local resource status = %d, want 404", w.Code)
	}
}

func TestPublicShare_RevokeDisablesAccess(t *testing.T) {
	handler, cleanup := setupSQLiteShareTestHandler(t)
	defer cleanup()

	pageID := createShareTestPage(t, handler)
	router := setupShareRouter(handler)
	share := createShareViaAPI(t, router, pageID)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/shares/"+strconv.FormatInt(share.ID, 10), nil)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("revoke status = %d, body: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, share.SnapshotURL, nil)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("revoked share status = %d, want 404", w.Code)
	}
}

func TestPublicShare_BypassesBasicAuthButManagementDoesNot(t *testing.T) {
	handler, cleanup := setupSQLiteShareTestHandler(t)
	defer cleanup()

	pageID := createShareTestPage(t, handler)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	SetupRoutes(router, handler, &config.AuthConfig{Password: "secret"}, &config.ServerConfig{}, "test", "")

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/pages/"+strconv.FormatInt(pageID, 10)+"/shares", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("management API without auth status = %d, want 401", w.Code)
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/pages/"+strconv.FormatInt(pageID, 10)+"/shares", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("wayback", "secret")
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("management API with auth status = %d, body: %s", w.Code, w.Body.String())
	}
	var share models.ShareResponse
	if err := json.Unmarshal(w.Body.Bytes(), &share); err != nil {
		t.Fatalf("Unmarshal share response failed: %v", err)
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, share.SnapshotURL, nil)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("public share without auth status = %d, body: %s", w.Code, w.Body.String())
	}
}
