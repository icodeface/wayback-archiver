package storage

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"wayback/internal/models"
)

func TestProcessCaptureAsync_ConcurrentIdenticalRequestsReuseSinglePage(t *testing.T) {
	dedup, db, _ := newFrameCaptureTestDeduplicator(t)
	defer func() {
		dedup.WaitForBackgroundTasks()
		db.Close()
	}()

	req := &models.CaptureRequest{
		URL:   fmt.Sprintf("https://page-concurrency-%d.example.com", time.Now().UnixNano()),
		Title: "concurrent page",
		HTML:  "<html><body>same page content</body></html>",
	}

	enteredCreate := make(chan struct{}, 2)
	releaseFirst := make(chan struct{})
	var hookCalls atomic.Int32
	dedup.testBeforePageCreate = func(url, contentHash string) {
		if url != req.URL {
			return
		}
		enteredCreate <- struct{}{}
		if hookCalls.Add(1) == 1 {
			<-releaseFirst
		}
	}

	type captureResult struct {
		pageID int64
		action string
		err    error
	}
	results := make(chan captureResult, 2)

	go func() {
		pageID, action, err := dedup.ProcessCaptureAsync(req)
		results <- captureResult{pageID: pageID, action: action, err: err}
	}()

	select {
	case <-enteredCreate:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first capture to enter create critical section")
	}

	secondStarted := make(chan struct{})
	go func() {
		close(secondStarted)
		pageID, action, err := dedup.ProcessCaptureAsync(req)
		results <- captureResult{pageID: pageID, action: action, err: err}
	}()
	<-secondStarted

	select {
	case <-enteredCreate:
		t.Fatal("second identical capture entered create critical section before first completed")
	case <-time.After(150 * time.Millisecond):
	}

	close(releaseFirst)

	createdCount := 0
	unchangedCount := 0
	pageIDs := make(map[int64]struct{}, 2)
	var pageID int64
	for i := 0; i < 2; i++ {
		result := <-results
		if result.err != nil {
			t.Fatalf("ProcessCaptureAsync failed: %v", result.err)
		}
		pageIDs[result.pageID] = struct{}{}
		pageID = result.pageID
		switch result.action {
		case models.ArchiveActionCreated:
			createdCount++
		case models.ArchiveActionUnchanged:
			unchangedCount++
		default:
			t.Fatalf("unexpected action %q", result.action)
		}
	}

	if hookCalls.Load() != 1 {
		t.Fatalf("page create critical section should run once, got %d", hookCalls.Load())
	}
	if len(pageIDs) != 1 {
		t.Fatalf("expected both captures to reuse one page record, got %d IDs", len(pageIDs))
	}
	if createdCount != 1 || unchangedCount != 1 {
		t.Fatalf("expected one created and one unchanged result, got created=%d unchanged=%d", createdCount, unchangedCount)
	}

	t.Cleanup(func() {
		_ = db.DeletePage(pageID)
	})

	pages, err := db.GetPagesByURL(req.URL)
	if err != nil {
		t.Fatalf("GetPagesByURL failed: %v", err)
	}
	if len(pages) != 1 {
		t.Fatalf("expected 1 stored page snapshot, got %d", len(pages))
	}
}

func TestProcessResource_ConcurrentSameURLReusesSingleResourceRecord(t *testing.T) {
	dedup, db, fs := newFrameCaptureTestDeduplicator(t)
	defer db.Close()

	var upstreamHits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		w.Header().Set("Content-Type", "text/css")
		w.Header().Set("Cache-Control", "public, max-age=60")
		_, _ = w.Write([]byte("body { color: red; }"))
	}))
	defer server.Close()

	baseURL := routeStorageHTTPClientToServer(t, fs, server)
	resourceURL := fmt.Sprintf("%s/style.css?nonce=%d", baseURL, time.Now().UnixNano())
	pageURL := baseURL + "/page"

	enteredCreate := make(chan struct{}, 2)
	releaseFirst := make(chan struct{})
	var hookCalls atomic.Int32
	dedup.testBeforeResourceCreate = func(url string) {
		if url != resourceURL {
			return
		}
		enteredCreate <- struct{}{}
		if hookCalls.Add(1) == 1 {
			<-releaseFirst
		}
	}

	type resourceResult struct {
		resourceID int64
		filePath   string
		err        error
	}
	results := make(chan resourceResult, 2)

	go func() {
		resourceID, filePath, _, err := dedup.ProcessResource(resourceURL, "css", pageURL, nil, nil)
		results <- resourceResult{resourceID: resourceID, filePath: filePath, err: err}
	}()

	select {
	case <-enteredCreate:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first resource to enter create critical section")
	}

	secondStarted := make(chan struct{})
	go func() {
		close(secondStarted)
		resourceID, filePath, _, err := dedup.ProcessResource(resourceURL, "css", pageURL, nil, nil)
		results <- resourceResult{resourceID: resourceID, filePath: filePath, err: err}
	}()
	<-secondStarted

	select {
	case <-enteredCreate:
		t.Fatal("second identical resource entered create critical section before first completed")
	case <-time.After(150 * time.Millisecond):
	}

	close(releaseFirst)

	first := <-results
	second := <-results
	if first.err != nil {
		t.Fatalf("first ProcessResource failed: %v", first.err)
	}
	if second.err != nil {
		t.Fatalf("second ProcessResource failed: %v", second.err)
	}
	if first.resourceID != second.resourceID {
		t.Fatalf("expected identical resource ID under concurrency, got %d and %d", first.resourceID, second.resourceID)
	}
	if first.filePath != second.filePath {
		t.Fatalf("expected identical file path under concurrency, got %q and %q", first.filePath, second.filePath)
	}
	if hookCalls.Load() != 2 {
		t.Fatalf("resource critical section should be entered twice serially, got %d", hookCalls.Load())
	}
	if upstreamHits.Load() != 1 {
		t.Fatalf("expected one upstream download after serialized reuse, got %d", upstreamHits.Load())
	}
}
