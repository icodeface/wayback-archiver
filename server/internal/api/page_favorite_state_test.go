package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"wayback/internal/database"
	"wayback/internal/models"
)

func TestListPagesIncludesFavoriteState(t *testing.T) {
	db, err := database.NewSQLite(filepath.Join(t.TempDir(), "wayback.db"))
	if err != nil {
		t.Fatalf("NewSQLite failed: %v", err)
	}
	defer db.Close()

	favoritedID, err := db.CreatePage("https://example.com/favorited", "Favorited", "html/favorited.html", "hash-favorited", time.Now())
	if err != nil {
		t.Fatalf("CreatePage favorited failed: %v", err)
	}
	normalID, err := db.CreatePage("https://example.com/normal", "Normal", "html/normal.html", "hash-normal", time.Now())
	if err != nil {
		t.Fatalf("CreatePage normal failed: %v", err)
	}
	if err := db.AddFavorite(favoritedID); err != nil {
		t.Fatalf("AddFavorite failed: %v", err)
	}

	handler := &Handler{db: db}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/pages", handler.ListPages)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/pages?limit=10", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	var response struct {
		Pages []models.Page `json:"pages"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	states := make(map[int64]*bool, len(response.Pages))
	for _, page := range response.Pages {
		states[page.ID] = page.IsFavorite
	}
	assertFavoriteState(t, states, favoritedID, true)
	assertFavoriteState(t, states, normalID, false)
}

func TestSearchPagesIncludesFavoriteState(t *testing.T) {
	db, err := database.NewSQLite(filepath.Join(t.TempDir(), "wayback.db"))
	if err != nil {
		t.Fatalf("NewSQLite failed: %v", err)
	}
	defer db.Close()

	pageID, err := db.CreatePage("https://example.com/search-favorite", "Search Favorite", "html/search-favorite.html", "hash-search-favorite", time.Now())
	if err != nil {
		t.Fatalf("CreatePage failed: %v", err)
	}
	if err := db.UpdatePageBodyText(pageID, "searchable favorite body"); err != nil {
		t.Fatalf("UpdatePageBodyText failed: %v", err)
	}
	if err := db.AddFavorite(pageID); err != nil {
		t.Fatalf("AddFavorite failed: %v", err)
	}

	handler := &Handler{db: db}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/search", handler.SearchPages)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/search?q=searchable", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	var response struct {
		Pages []models.Page `json:"pages"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	if len(response.Pages) != 1 || response.Pages[0].IsFavorite == nil || !*response.Pages[0].IsFavorite {
		t.Fatalf("expected search result to be favorited, got %+v", response.Pages)
	}
}

func TestGetPageIncludesFavoriteState(t *testing.T) {
	db, err := database.NewSQLite(filepath.Join(t.TempDir(), "wayback.db"))
	if err != nil {
		t.Fatalf("NewSQLite failed: %v", err)
	}
	defer db.Close()

	pageID, err := db.CreatePage("https://example.com/detail-favorite", "Detail Favorite", "html/detail-favorite.html", "hash-detail-favorite", time.Now())
	if err != nil {
		t.Fatalf("CreatePage failed: %v", err)
	}
	if err := db.AddFavorite(pageID); err != nil {
		t.Fatalf("AddFavorite failed: %v", err)
	}

	handler := &Handler{db: db}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/pages/:id", handler.GetPage)

	recorder := httptest.NewRecorder()
	path := "/api/pages/" + strconv.FormatInt(pageID, 10)
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	var page models.Page
	if err := json.Unmarshal(recorder.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	assertFavoriteState(t, map[int64]*bool{page.ID: page.IsFavorite}, pageID, true)
}

func assertFavoriteState(t *testing.T, states map[int64]*bool, pageID int64, want bool) {
	t.Helper()
	state, ok := states[pageID]
	if !ok || state == nil {
		t.Fatalf("page %d missing is_favorite state", pageID)
	}
	if *state != want {
		t.Errorf("page %d is_favorite = %v, want %v", pageID, *state, want)
	}
}
