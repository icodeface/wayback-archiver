package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func setupViewRouter(handler *Handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/view/:id", handler.ViewPage)
	r.GET("/view/:id/md", handler.ViewPageMarkdown)
	return r
}

func TestViewPage_InvalidID(t *testing.T) {
	handler, cleanup := setupTestHandler(t)
	defer cleanup()

	router := setupViewRouter(handler)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/view/not-a-number", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid page id, got %d: %s", w.Code, w.Body.String())
	}
}

func TestViewPage_DatabaseErrorReturnsInternalServerError(t *testing.T) {
	handler, cleanup := setupTestHandler(t)
	defer cleanup()

	router := setupViewRouter(handler)
	_ = handler.db.Close()

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/view/1", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for database error, got %d: %s", w.Code, w.Body.String())
	}
}

func TestViewPageContentSecurityPolicy_AllowsOnlyNonceScriptAndSelfConnect(t *testing.T) {
	got := viewPageContentSecurityPolicy("test-nonce")

	for _, want := range []string{
		`script-src 'nonce-test-nonce'`,
		`connect-src 'self'`,
		`object-src 'none'`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("CSP missing %q: %s", want, got)
		}
	}

	for _, unwanted := range []string{
		`script-src 'unsafe-inline'`,
		`connect-src *`,
		`connect-src http:`,
		`connect-src https:`,
	} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("CSP contains unsafe directive %q: %s", unwanted, got)
		}
	}
}

func TestViewPageMarkdown_InvalidID(t *testing.T) {
	handler, cleanup := setupTestHandler(t)
	defer cleanup()

	router := setupViewRouter(handler)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/view/not-a-number/md", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid page id, got %d: %s", w.Code, w.Body.String())
	}
}

func TestViewPageMarkdown_ReturnsMarkdown(t *testing.T) {
	handler, cleanup := setupTestHandler(t)
	defer cleanup()

	htmlPath := "html/test/view-markdown.html"
	fullPath := filepath.Join(handler.dataDir, htmlPath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(fullPath, []byte("<html><body><h1>Saved Title</h1><p>Hello <strong>Markdown</strong>.</p></body></html>"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	pageID, err := handler.db.CreatePage(
		"https://view-markdown-test.example.com/page",
		"View Markdown",
		htmlPath,
		strings.Repeat("c", 64),
		time.Now(),
	)
	if err != nil {
		t.Fatalf("CreatePage failed: %v", err)
	}
	defer handler.db.DeletePage(pageID)

	router := setupViewRouter(handler)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/view/"+strconv.FormatInt(pageID, 10)+"/md", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); got != "text/markdown; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want text/markdown; charset=utf-8", got)
	}
	if body := w.Body.String(); !strings.Contains(body, "# Saved Title") || !strings.Contains(body, "**Markdown**") {
		t.Fatalf("unexpected markdown body:\n%s", body)
	}
}
