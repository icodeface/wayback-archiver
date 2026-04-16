package storage

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"wayback/internal/models"
)

func waitForSnapshotState(t *testing.T, db interface {
	GetPageByID(string) (*models.Page, error)
}, pageID int64, want string) *models.Page {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		page, err := db.GetPageByID(fmt.Sprintf("%d", pageID))
		if err == nil && page != nil && page.SnapshotState == want {
			return page
		}
		time.Sleep(10 * time.Millisecond)
	}

	page, err := db.GetPageByID(fmt.Sprintf("%d", pageID))
	if err != nil {
		t.Fatalf("GetPageByID failed: %v", err)
	}
	if page == nil {
		t.Fatalf("expected page %d to exist", pageID)
	}
	t.Fatalf("page snapshot_state = %q, want %q", page.SnapshotState, want)
	return nil
}

func TestCloneCaptureRequest_CopiesCookies(t *testing.T) {
	original := &models.CaptureRequest{
		URL:   "https://example.com/page",
		Title: "cookie clone",
		HTML:  "<html></html>",
		Cookies: []models.CaptureCookie{{
			Name:   "session",
			Value:  "abc",
			Domain: ".example.com",
			Path:   "/",
		}},
	}

	cloned := cloneCaptureRequest(original)
	if cloned == nil {
		t.Fatal("cloneCaptureRequest returned nil")
	}
	if len(cloned.Cookies) != 1 {
		t.Fatalf("expected 1 cloned cookie, got %d", len(cloned.Cookies))
	}
	if cloned.Cookies[0] != original.Cookies[0] {
		t.Fatalf("cloned cookie = %+v, want %+v", cloned.Cookies[0], original.Cookies[0])
	}

	cloned.Cookies[0].Value = "changed"
	if original.Cookies[0].Value != "abc" {
		t.Fatalf("mutating cloned cookies should not affect original request")
	}
}

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

func TestProcessCaptureAsync_PreservesCookiesForBackgroundDownloads(t *testing.T) {
	dedup, db, fs := newFrameCaptureTestDeduplicator(t)
	defer func() {
		dedup.WaitForBackgroundTasks()
		db.Close()
	}()

	var receivedCookie atomic.Value
	receivedCookie.Store("")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedCookie.Store(r.Header.Get("Cookie"))
		w.Header().Set("Content-Type", "text/css")
		_, _ = w.Write([]byte("body { color: red; }"))
	}))
	defer server.Close()

	baseURL := routeStorageHTTPClientToServer(t, fs, server)
	pageURL := fmt.Sprintf("%s/page-%d", baseURL, time.Now().UnixNano())
	cssURL := baseURL + "/auth.css"
	req := &models.CaptureRequest{
		URL:   pageURL,
		Title: "async create with cookies",
		HTML:  `<html><head><link rel="stylesheet" href="` + cssURL + `"></head><body>async create with cookies</body></html>`,
		Cookies: []models.CaptureCookie{{
			Name:   "session",
			Value:  "abc123",
			Domain: "archive-test.example",
			Path:   "/",
		}},
	}

	pageID, action, err := dedup.ProcessCaptureAsync(req)
	if err != nil {
		t.Fatalf("ProcessCaptureAsync failed: %v", err)
	}
	if action != models.ArchiveActionCreated {
		t.Fatalf("action = %q, want %q", action, models.ArchiveActionCreated)
	}
	defer db.DeletePage(pageID)

	dedup.WaitForBackgroundTasks()

	if got := receivedCookie.Load().(string); got != "session=abc123" {
		t.Fatalf("background resource download cookie header = %q, want %q", got, "session=abc123")
	}
}

func TestProcessCaptureAsync_ReusesPendingCreateWithoutRestartingFinalize(t *testing.T) {
	dedup, db, _ := newFrameCaptureTestDeduplicator(t)
	defer func() {
		dedup.WaitForBackgroundTasks()
		db.Close()
	}()

	req := &models.CaptureRequest{
		URL:   fmt.Sprintf("https://pending-create-reuse.example.com/%d", time.Now().UnixNano()),
		Title: "pending create reuse",
		HTML:  "<html><body>pending create reuse</body></html>",
	}

	startedFinalize := make(chan struct{}, 1)
	releaseFinalize := make(chan struct{})
	var finalizeCalls atomic.Int32
	dedup.testBeforeCreateFinalize = func(pageID int64, htmlPath string, resourceIDs []int64) error {
		call := finalizeCalls.Add(1)
		if call == 1 {
			startedFinalize <- struct{}{}
			<-releaseFinalize
		}
		return nil
	}

	pageID, action, err := dedup.ProcessCaptureAsync(req)
	if err != nil {
		t.Fatalf("ProcessCaptureAsync failed: %v", err)
	}
	if action != models.ArchiveActionCreated {
		t.Fatalf("action = %q, want %q", action, models.ArchiveActionCreated)
	}
	defer db.DeletePage(pageID)

	select {
	case <-startedFinalize:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first async finalize to start")
	}

	pageID2, action2, err := dedup.ProcessCaptureAsync(req)
	if err != nil {
		t.Fatalf("second ProcessCaptureAsync failed: %v", err)
	}
	if action2 != models.ArchiveActionCreated {
		t.Fatalf("second action = %q, want %q", action2, models.ArchiveActionCreated)
	}
	if pageID2 != pageID {
		t.Fatalf("second pageID = %d, want %d", pageID2, pageID)
	}

	page, err := db.GetPageByID(fmt.Sprintf("%d", pageID))
	if err != nil {
		t.Fatalf("GetPageByID failed: %v", err)
	}
	if page == nil {
		t.Fatalf("expected page %d to exist", pageID)
	}
	if page.SnapshotState != models.SnapshotStatePending {
		t.Fatalf("page snapshot_state = %q, want %q while finalize is blocked", page.SnapshotState, models.SnapshotStatePending)
	}

	close(releaseFinalize)
	dedup.WaitForBackgroundTasks()
	page = waitForSnapshotState(t, db, pageID, models.SnapshotStateReady)
	if page.SnapshotState != models.SnapshotStateReady {
		t.Fatalf("page snapshot_state = %q, want %q", page.SnapshotState, models.SnapshotStateReady)
	}
	if finalizeCalls.Load() != 1 {
		t.Fatalf("expected pending duplicate capture to reuse existing finalize, got %d finalize calls", finalizeCalls.Load())
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

func TestProcessCaptureAsync_RetriesFailedCreateForSameURLAndHash(t *testing.T) {
	dedup, db, _ := newFrameCaptureTestDeduplicator(t)
	defer func() {
		dedup.WaitForBackgroundTasks()
		db.Close()
	}()

	req := &models.CaptureRequest{
		URL:   fmt.Sprintf("https://async-retry-create.example.com/%d", time.Now().UnixNano()),
		Title: "retry async create",
		HTML:  "<html><body>retry async create</body></html>",
	}

	shouldFail := true
	dedup.testBeforeCreateFinalize = func(pageID int64, htmlPath string, resourceIDs []int64) error {
		if shouldFail {
			shouldFail = false
			return errors.New("forced async finalize failure")
		}
		return nil
	}

	pageID, action, err := dedup.ProcessCaptureAsync(req)
	if err != nil {
		t.Fatalf("ProcessCaptureAsync failed: %v", err)
	}
	if action != models.ArchiveActionCreated {
		t.Fatalf("action = %q, want %q", action, models.ArchiveActionCreated)
	}
	defer db.DeletePage(pageID)

	dedup.WaitForBackgroundTasks()
	page := waitForSnapshotState(t, db, pageID, models.SnapshotStateFailed)
	if page.SnapshotState != models.SnapshotStateFailed {
		t.Fatalf("page snapshot_state = %q, want %q", page.SnapshotState, models.SnapshotStateFailed)
	}

	retryPageID, retryAction, err := dedup.ProcessCaptureAsync(req)
	if err != nil {
		t.Fatalf("retry ProcessCaptureAsync failed: %v", err)
	}
	if retryAction != models.ArchiveActionCreated {
		t.Fatalf("retry action = %q, want %q", retryAction, models.ArchiveActionCreated)
	}
	if retryPageID != pageID {
		t.Fatalf("retry pageID = %d, want %d", retryPageID, pageID)
	}

	dedup.WaitForBackgroundTasks()
	page = waitForSnapshotState(t, db, pageID, models.SnapshotStateReady)
	if page.SnapshotState != models.SnapshotStateReady {
		t.Fatalf("page snapshot_state = %q, want %q", page.SnapshotState, models.SnapshotStateReady)
	}

	pages, err := db.GetPagesByURL(req.URL)
	if err != nil {
		t.Fatalf("GetPagesByURL failed: %v", err)
	}
	if len(pages) != 1 {
		t.Fatalf("expected 1 stored page snapshot after retry, got %d", len(pages))
	}
	if pages[0].SnapshotState != models.SnapshotStateReady {
		t.Fatalf("stored page snapshot_state = %q, want %q", pages[0].SnapshotState, models.SnapshotStateReady)
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
