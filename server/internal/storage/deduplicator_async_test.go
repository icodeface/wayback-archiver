package storage

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"wayback/internal/models"
)

func TestProcessCaptureAsync_ReturnsBeforeResourceDownloadCompletes(t *testing.T) {
	dedup, db, fs := newFrameCaptureTestDeduplicator(t)
	defer func() {
		dedup.WaitForBackgroundTasks()
		db.Close()
	}()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(250 * time.Millisecond)
		w.Header().Set("Content-Type", "text/css")
		_, _ = w.Write([]byte("body { color: red; }"))
	}))
	defer server.Close()

	baseURL := routeStorageHTTPClientToServer(t, fs, server)
	pageURL := fmt.Sprintf("%s/page-%d", baseURL, time.Now().UnixNano())
	cssURL := baseURL + "/slow.css"
	req := &models.CaptureRequest{
		URL:   pageURL,
		Title: "async create",
		HTML:  `<html><head><link rel="stylesheet" href="` + cssURL + `"></head><body>async create</body></html>`,
	}

	start := time.Now()
	pageID, action, err := dedup.ProcessCaptureAsync(req)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("ProcessCaptureAsync failed: %v", err)
	}
	if action != models.ArchiveActionCreated {
		t.Fatalf("action = %q, want %q", action, models.ArchiveActionCreated)
	}
	if elapsed >= 150*time.Millisecond {
		t.Fatalf("ProcessCaptureAsync took %v, expected immediate return before slow download", elapsed)
	}
	defer db.DeletePage(pageID)

	dedup.WaitForBackgroundTasks()

	resources, err := db.GetResourcesByPageID(pageID)
	if err != nil {
		t.Fatalf("GetResourcesByPageID failed: %v", err)
	}
	if len(resources) != 1 {
		t.Fatalf("expected 1 linked resource after background finalize, got %d", len(resources))
	}

	page, err := db.GetPageByID(fmt.Sprintf("%d", pageID))
	if err != nil {
		t.Fatalf("GetPageByID failed: %v", err)
	}
	if page == nil {
		t.Fatalf("expected page %d to exist", pageID)
	}

	htmlContent, err := os.ReadFile(filepath.Join(fs.baseDir, page.HTMLPath))
	if err != nil {
		t.Fatalf("ReadFile page html failed: %v", err)
	}
	if !strings.Contains(string(htmlContent), archiveProxyURL(pageID, page.CapturedAt.Format("20060102150405"), cssURL)) {
		t.Fatalf("background finalize should rewrite CSS URL to archive proxy")
	}
}

func TestUpdateCaptureAsync_ReturnsBeforeBackgroundFinalizeCompletes(t *testing.T) {
	dedup, db, fs := newFrameCaptureTestDeduplicator(t)
	defer func() {
		dedup.WaitForBackgroundTasks()
		db.Close()
	}()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/style-v1.css":
			w.Header().Set("Content-Type", "text/css")
			_, _ = w.Write([]byte("body { color: red; }"))
		case "/style-v2.css":
			time.Sleep(250 * time.Millisecond)
			w.Header().Set("Content-Type", "text/css")
			_, _ = w.Write([]byte("body { color: blue; }"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	baseURL := routeStorageHTTPClientToServer(t, fs, server)
	pageURL := fmt.Sprintf("%s/page-%d", baseURL, time.Now().UnixNano())
	createReq := &models.CaptureRequest{
		URL:   pageURL,
		Title: "before async update",
		HTML:  `<html><head><link rel="stylesheet" href="` + baseURL + `/style-v1.css"></head><body>before async update</body></html>`,
	}

	pageID, action, err := dedup.ProcessCapture(createReq)
	if err != nil {
		t.Fatalf("ProcessCapture failed: %v", err)
	}
	if action != models.ArchiveActionCreated {
		t.Fatalf("action = %q, want %q", action, models.ArchiveActionCreated)
	}
	defer db.DeletePage(pageID)

	updateReq := &models.CaptureRequest{
		URL:   pageURL,
		Title: "after async update",
		HTML:  `<html><head><link rel="stylesheet" href="` + baseURL + `/style-v2.css"></head><body>after async update</body></html>`,
	}

	start := time.Now()
	action, err = dedup.UpdateCaptureAsync(pageID, updateReq)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("UpdateCaptureAsync failed: %v", err)
	}
	if action != models.ArchiveActionUpdated {
		t.Fatalf("action = %q, want %q", action, models.ArchiveActionUpdated)
	}
	if elapsed >= 150*time.Millisecond {
		t.Fatalf("UpdateCaptureAsync took %v, expected immediate return before slow finalize", elapsed)
	}

	page, err := db.GetPageByID(fmt.Sprintf("%d", pageID))
	if err != nil {
		t.Fatalf("GetPageByID(before finalize) failed: %v", err)
	}
	if page == nil {
		t.Fatalf("expected page %d to exist", pageID)
	}
	if page.Title != "before async update" {
		t.Fatalf("page title should remain old value until background finalize, got %q", page.Title)
	}

	dedup.WaitForBackgroundTasks()

	page, err = db.GetPageByID(fmt.Sprintf("%d", pageID))
	if err != nil {
		t.Fatalf("GetPageByID(after finalize) failed: %v", err)
	}
	if page == nil {
		t.Fatalf("expected page %d to exist after finalize", pageID)
	}
	if page.Title != "after async update" {
		t.Fatalf("page title = %q, want %q", page.Title, "after async update")
	}
}

func TestUpdateCaptureAsync_SecondUpdateWinsOverStaleBackgroundTask(t *testing.T) {
	dedup, db, fs := newFrameCaptureTestDeduplicator(t)
	defer func() {
		dedup.WaitForBackgroundTasks()
		db.Close()
	}()

	v2Started := make(chan struct{}, 1)
	releaseV2 := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/style-v1.css":
			w.Header().Set("Content-Type", "text/css")
			_, _ = w.Write([]byte("body { color: red; }"))
		case "/style-v2.css":
			select {
			case v2Started <- struct{}{}:
			default:
			}
			<-releaseV2
			w.Header().Set("Content-Type", "text/css")
			_, _ = w.Write([]byte("body { color: blue; }"))
		case "/style-v3.css":
			w.Header().Set("Content-Type", "text/css")
			_, _ = w.Write([]byte("body { color: green; }"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	baseURL := routeStorageHTTPClientToServer(t, fs, server)
	pageURL := fmt.Sprintf("%s/page-%d", baseURL, time.Now().UnixNano())
	createReq := &models.CaptureRequest{
		URL:   pageURL,
		Title: "before async race",
		HTML:  `<html><head><link rel="stylesheet" href="` + baseURL + `/style-v1.css"></head><body>before async race</body></html>`,
	}

	pageID, action, err := dedup.ProcessCapture(createReq)
	if err != nil {
		t.Fatalf("ProcessCapture failed: %v", err)
	}
	if action != models.ArchiveActionCreated {
		t.Fatalf("action = %q, want %q", action, models.ArchiveActionCreated)
	}
	defer db.DeletePage(pageID)

	firstUpdate := &models.CaptureRequest{
		URL:   pageURL,
		Title: "stale async update",
		HTML:  `<html><head><link rel="stylesheet" href="` + baseURL + `/style-v2.css"></head><body>first update loses</body></html>`,
	}
	secondUpdate := &models.CaptureRequest{
		URL:   pageURL,
		Title: "winning async update",
		HTML:  `<html><head><link rel="stylesheet" href="` + baseURL + `/style-v3.css"></head><body>second update wins</body></html>`,
	}

	action, err = dedup.UpdateCaptureAsync(pageID, firstUpdate)
	if err != nil {
		t.Fatalf("first UpdateCaptureAsync failed: %v", err)
	}
	if action != models.ArchiveActionUpdated {
		t.Fatalf("first action = %q, want %q", action, models.ArchiveActionUpdated)
	}

	select {
	case <-v2Started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first async update to start slow resource download")
	}

	action, err = dedup.UpdateCaptureAsync(pageID, secondUpdate)
	if err != nil {
		t.Fatalf("second UpdateCaptureAsync failed: %v", err)
	}
	if action != models.ArchiveActionUpdated {
		t.Fatalf("second action = %q, want %q", action, models.ArchiveActionUpdated)
	}

	close(releaseV2)
	dedup.WaitForBackgroundTasks()

	page, err := db.GetPageByID(fmt.Sprintf("%d", pageID))
	if err != nil {
		t.Fatalf("GetPageByID failed: %v", err)
	}
	if page == nil {
		t.Fatalf("expected page %d to exist", pageID)
	}
	if page.Title != "winning async update" {
		t.Fatalf("page title = %q, want %q", page.Title, "winning async update")
	}

	resources, err := db.GetResourcesByPageID(pageID)
	if err != nil {
		t.Fatalf("GetResourcesByPageID failed: %v", err)
	}
	if len(resources) != 1 {
		t.Fatalf("expected 1 linked resource after competing updates, got %d", len(resources))
	}
	if resources[0].URL != baseURL+"/style-v3.css" {
		t.Fatalf("linked resource URL = %q, want %q", resources[0].URL, baseURL+"/style-v3.css")
	}

	htmlContent, err := os.ReadFile(filepath.Join(fs.baseDir, page.HTMLPath))
	if err != nil {
		t.Fatalf("ReadFile page html failed: %v", err)
	}
	html := string(htmlContent)
	if !strings.Contains(html, "second update wins") {
		t.Fatalf("final HTML should contain winning update body, got: %s", html)
	}
	if strings.Contains(html, "first update loses") {
		t.Fatalf("stale async update should not overwrite final HTML, got: %s", html)
	}
}
