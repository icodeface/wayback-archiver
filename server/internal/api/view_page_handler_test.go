package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func setupViewRouter(handler *Handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/view/:id", handler.ViewPage)
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
