package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

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

func TestSearchPages_InvalidLimit(t *testing.T) {
	handler, cleanup := setupTestHandler(t)
	defer cleanup()
	router := setupPagesRouter(handler)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/search?q=test&limit=0", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid limit, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSearchPages_PaginatedResponse(t *testing.T) {
	handler, cleanup := setupTestHandler(t)
	defer cleanup()
	router := setupPagesRouter(handler)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	domain := "api-search-pagination-" + suffix + ".example.com"
	token := "api-search-pagination-token-" + suffix
	now := time.Now()

	for i := 0; i < 3; i++ {
		pageID, err := handler.db.CreatePage(
			fmt.Sprintf("https://%s/page-%d", domain, i),
			fmt.Sprintf("API Search Pagination %d", i),
			fmt.Sprintf("html/test/api-search-pagination-%s-%d.html", suffix, i),
			fmt.Sprintf("%064d", i+2000),
			now.Add(time.Duration(i)*time.Second),
		)
		if err != nil {
			t.Fatalf("CreatePage(%d) failed: %v", i, err)
		}
		defer handler.db.DeletePage(pageID)

		if err := handler.db.UpdatePageBodyText(pageID, token); err != nil {
			t.Fatalf("UpdatePageBodyText(%d) failed: %v", i, err)
		}
	}

	w := httptest.NewRecorder()
	reqURL := fmt.Sprintf(
		"/api/search?q=%s&domain=%s&limit=2&offset=2",
		url.QueryEscape(token),
		url.QueryEscape(domain),
	)
	req, _ := http.NewRequest(http.MethodGet, reqURL, nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var response struct {
		Pages []struct {
			ID int64 `json:"id"`
		} `json:"pages"`
		Total  int `json:"total"`
		Limit  int `json:"limit"`
		Offset int `json:"offset"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.Total != 3 {
		t.Fatalf("total = %d, want 3", response.Total)
	}
	if response.Limit != 2 || response.Offset != 2 {
		t.Fatalf("limit/offset = %d/%d, want 2/2", response.Limit, response.Offset)
	}
	if len(response.Pages) != 1 {
		t.Fatalf("pages length = %d, want 1", len(response.Pages))
	}
}
