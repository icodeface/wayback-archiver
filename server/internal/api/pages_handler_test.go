package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func setupPagesRouter(handler *Handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api")
	api.GET("/pages", handler.ListPages)
	api.GET("/pages/:id", handler.GetPage)
	api.GET("/pages/:id/content", handler.GetPageContent)
	api.DELETE("/pages/:id", handler.DeletePage)
	api.GET("/search", handler.SearchPages)
	return r
}

func TestGetPage_InvalidID(t *testing.T) {
	handler, cleanup := setupTestHandler(t)
	defer cleanup()
	router := setupPagesRouter(handler)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/pages/not-a-number", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid page ID, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeletePage_InvalidID(t *testing.T) {
	handler, cleanup := setupTestHandler(t)
	defer cleanup()
	router := setupPagesRouter(handler)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodDelete, "/api/pages/not-a-number", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid page ID, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetPageContent_InvalidID(t *testing.T) {
	handler, cleanup := setupTestHandler(t)
	defer cleanup()
	router := setupPagesRouter(handler)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/pages/not-a-number/content", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid page ID, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListPages_InvalidLimit(t *testing.T) {
	handler, cleanup := setupTestHandler(t)
	defer cleanup()
	router := setupPagesRouter(handler)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/pages?limit=not-a-number", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid limit, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSearchPages_InvalidFromDate(t *testing.T) {
	handler, cleanup := setupTestHandler(t)
	defer cleanup()
	router := setupPagesRouter(handler)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/search?q=test&from=2026-99-99", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid from date, got %d: %s", w.Code, w.Body.String())
	}
}
