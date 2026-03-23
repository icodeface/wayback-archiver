package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"wayback/internal/config"
	"wayback/internal/database"
	"wayback/internal/models"
	"wayback/internal/storage"
)

func setupTestHandler(t *testing.T) (*Handler, func()) {
	t.Helper()
	db, err := database.New("localhost", "5432", "postgres", "", "wayback")
	if err != nil {
		t.Skipf("Skipping integration test (cannot connect to DB): %v", err)
	}

	dataDir := t.TempDir()
	fs := storage.NewFileStorage(dataDir)
	dedup := storage.NewDeduplicator(db, fs, config.ResourceConfig{
		Workers:         4,
		CacheSizeMB:     100,
		DownloadTimeout: 30,
	})
	handler := NewHandler(dedup, db, dataDir, nil)

	cleanup := func() {
		db.Close()
	}
	return handler, cleanup
}

func setupRouter(handler *Handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api")
	api.POST("/archive", handler.ArchivePage)
	api.PUT("/archive/:id", handler.UpdatePage)
	return r
}

func TestArchivePage_ReturnsPageIDAndAction(t *testing.T) {
	handler, cleanup := setupTestHandler(t)
	defer cleanup()
	router := setupRouter(handler)

	req := models.CaptureRequest{
		URL:   "http://test-archive-handler.example.com/page1",
		Title: "Test Page",
		HTML:  "<html><body>Hello World</body></html>",
	}
	body, _ := json.Marshal(req)

	w := httptest.NewRecorder()
	httpReq, _ := http.NewRequest("POST", "/api/archive", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp models.ArchiveResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp.Status != "success" {
		t.Errorf("status = %q, want %q", resp.Status, "success")
	}
	if resp.PageID <= 0 {
		t.Errorf("page_id should be positive, got %d", resp.PageID)
	}
	if resp.Action != models.ArchiveActionCreated {
		t.Errorf("action = %q, want %q", resp.Action, models.ArchiveActionCreated)
	}

	// Cleanup
	defer handler.db.DeletePage(resp.PageID)

	// Archive same content again — should be unchanged
	w2 := httptest.NewRecorder()
	httpReq2, _ := http.NewRequest("POST", "/api/archive", bytes.NewReader(body))
	httpReq2.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w2, httpReq2)

	var resp2 models.ArchiveResponse
	if err := json.Unmarshal(w2.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("failed to parse second response: %v", err)
	}
	if resp2.Action != models.ArchiveActionUnchanged {
		t.Errorf("second archive action = %q, want %q", resp2.Action, models.ArchiveActionUnchanged)
	}
	if resp2.PageID != resp.PageID {
		t.Errorf("second archive page_id = %d, want %d", resp2.PageID, resp.PageID)
	}
}

func TestUpdatePage_UpdatesContent(t *testing.T) {
	handler, cleanup := setupTestHandler(t)
	defer cleanup()
	router := setupRouter(handler)

	// Create initial page
	req := models.CaptureRequest{
		URL:   "http://test-update-handler.example.com/page2",
		Title: "Original",
		HTML:  "<html><body>Original Content</body></html>",
	}
	body, _ := json.Marshal(req)

	w := httptest.NewRecorder()
	httpReq, _ := http.NewRequest("POST", "/api/archive", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, httpReq)

	var createResp models.ArchiveResponse
	json.Unmarshal(w.Body.Bytes(), &createResp)
	if createResp.PageID <= 0 {
		t.Fatalf("failed to create page: %s", w.Body.String())
	}
	defer handler.db.DeletePage(createResp.PageID)

	// Update with different content
	updateReq := models.CaptureRequest{
		URL:   "http://test-update-handler.example.com/page2",
		Title: "Updated",
		HTML:  "<html><body>Updated Content with more stuff</body></html>",
	}
	updateBody, _ := json.Marshal(updateReq)

	w2 := httptest.NewRecorder()
	httpReq2, _ := http.NewRequest("PUT", "/api/archive/"+strconv.FormatInt(createResp.PageID, 10), bytes.NewReader(updateBody))
	httpReq2.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w2, httpReq2)

	if w2.Code != http.StatusOK {
		t.Fatalf("update expected 200, got %d: %s", w2.Code, w2.Body.String())
	}

	var updateResp models.ArchiveResponse
	json.Unmarshal(w2.Body.Bytes(), &updateResp)

	if updateResp.Action != models.ArchiveActionUpdated {
		t.Errorf("update action = %q, want %q", updateResp.Action, models.ArchiveActionUpdated)
	}
	if updateResp.PageID != createResp.PageID {
		t.Errorf("update page_id = %d, want %d", updateResp.PageID, createResp.PageID)
	}

	// Verify DB was updated
	page, err := handler.db.GetPageByID(strconv.FormatInt(createResp.PageID, 10))
	if err != nil {
		t.Fatalf("GetPageByID failed: %v", err)
	}
	if page.Title != "Updated" {
		t.Errorf("title = %q, want %q", page.Title, "Updated")
	}
}

func TestUpdatePage_SameContentUnchanged(t *testing.T) {
	handler, cleanup := setupTestHandler(t)
	defer cleanup()
	router := setupRouter(handler)

	// Create initial page
	html := "<html><body>Same Content</body></html>"
	req := models.CaptureRequest{
		URL:   "http://test-update-unchanged.example.com/page3",
		Title: "Same",
		HTML:  html,
	}
	body, _ := json.Marshal(req)

	w := httptest.NewRecorder()
	httpReq, _ := http.NewRequest("POST", "/api/archive", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, httpReq)

	var createResp models.ArchiveResponse
	json.Unmarshal(w.Body.Bytes(), &createResp)
	defer handler.db.DeletePage(createResp.PageID)

	// Update with same content
	w2 := httptest.NewRecorder()
	httpReq2, _ := http.NewRequest("PUT", "/api/archive/"+strconv.FormatInt(createResp.PageID, 10), bytes.NewReader(body))
	httpReq2.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w2, httpReq2)

	var updateResp models.ArchiveResponse
	json.Unmarshal(w2.Body.Bytes(), &updateResp)

	if updateResp.Action != models.ArchiveActionUnchanged {
		t.Errorf("update with same content action = %q, want %q", updateResp.Action, models.ArchiveActionUnchanged)
	}
}

func TestUpdatePage_InvalidID(t *testing.T) {
	handler, cleanup := setupTestHandler(t)
	defer cleanup()
	router := setupRouter(handler)

	req := models.CaptureRequest{
		URL:   "http://test.example.com",
		Title: "Test",
		HTML:  "<html></html>",
	}
	body, _ := json.Marshal(req)

	w := httptest.NewRecorder()
	httpReq, _ := http.NewRequest("PUT", "/api/archive/notanumber", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid ID, got %d", w.Code)
	}
}

func TestUpdatePage_NonExistentPage(t *testing.T) {
	handler, cleanup := setupTestHandler(t)
	defer cleanup()
	router := setupRouter(handler)

	req := models.CaptureRequest{
		URL:   "http://test.example.com",
		Title: "Test",
		HTML:  "<html></html>",
	}
	body, _ := json.Marshal(req)

	w := httptest.NewRecorder()
	httpReq, _ := http.NewRequest("PUT", "/api/archive/999999999", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for non-existent page, got %d", w.Code)
	}
}
