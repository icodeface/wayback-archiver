package storage

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"wayback/internal/models"
)

func routeStorageHTTPClientToServer(t *testing.T, fs *FileStorage, server *httptest.Server) string {
	t.Helper()

	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("url.Parse(server.URL) failed: %v", err)
	}

	transport, ok := fs.httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("unexpected transport type %T", fs.httpClient.Transport)
	}

	cloned := transport.Clone()
	cloned.Proxy = nil
	cloned.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		var dialer net.Dialer
		return dialer.DialContext(ctx, network, target.Host)
	}
	fs.httpClient.Transport = cloned

	return "http://archive-test.example"
}

func TestProcessResource_SameURLChangedContentCreatesNewVersion(t *testing.T) {
	dedup, db, fs := newFrameCaptureTestDeduplicator(t)
	defer db.Close()

	content := "console.log('v1');"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(content))
	}))
	defer server.Close()

	baseURL := routeStorageHTTPClientToServer(t, fs, server)
	resourceURL := fmt.Sprintf("%s/app.js?nonce=%d", baseURL, time.Now().UnixNano())
	pageURL := baseURL + "/page"

	resourceID1, filePath1, _, err := dedup.ProcessResource(resourceURL, "js", pageURL, nil, nil)
	if err != nil {
		t.Fatalf("ProcessResource(v1) failed: %v", err)
	}

	content = "console.log('v2');"

	resourceID2, filePath2, _, err := dedup.ProcessResource(resourceURL, "js", pageURL, nil, nil)
	if err != nil {
		t.Fatalf("ProcessResource(v2) failed: %v", err)
	}

	if resourceID1 == resourceID2 {
		t.Fatalf("same URL with changed content should create a new resource record")
	}
	if filePath1 == filePath2 {
		t.Fatalf("same URL with changed content should store a new file version")
	}

	resource1, err := db.GetResourceByID(resourceID1)
	if err != nil {
		t.Fatalf("GetResourceByID(%d) failed: %v", resourceID1, err)
	}
	resource2, err := db.GetResourceByID(resourceID2)
	if err != nil {
		t.Fatalf("GetResourceByID(%d) failed: %v", resourceID2, err)
	}
	if resource1 == nil || resource2 == nil {
		t.Fatalf("expected both resource records to exist")
	}
	if resource1.ContentHash == resource2.ContentHash {
		t.Fatalf("content hash should differ after same-URL resource changes")
	}

	latest, err := db.GetResourceByURL(resourceURL)
	if err != nil {
		t.Fatalf("GetResourceByURL failed: %v", err)
	}
	if latest == nil || latest.ID != resourceID2 {
		t.Fatalf("latest resource for URL should be the new version")
	}

}

func TestProcessResource_FreshCacheSkipsRedownload(t *testing.T) {
	dedup, db, fs := newFrameCaptureTestDeduplicator(t)
	defer db.Close()

	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/css")
		w.Header().Set("Cache-Control", "public, max-age=60")
		_, _ = w.Write([]byte("body { color: red; }"))
	}))
	defer server.Close()

	baseURL := routeStorageHTTPClientToServer(t, fs, server)
	resourceURL := fmt.Sprintf("%s/style.css?nonce=%d", baseURL, time.Now().UnixNano())
	pageURL := baseURL + "/page"

	resourceID1, filePath1, _, err := dedup.ProcessResource(resourceURL, "css", pageURL, nil, nil)
	if err != nil {
		t.Fatalf("first ProcessResource failed: %v", err)
	}
	resourceID2, filePath2, _, err := dedup.ProcessResource(resourceURL, "css", pageURL, nil, nil)
	if err != nil {
		t.Fatalf("second ProcessResource failed: %v", err)
	}

	if hits.Load() != 1 {
		t.Fatalf("fresh cache should avoid a second download: got %d upstream requests, want 1", hits.Load())
	}
	if resourceID1 != resourceID2 {
		t.Fatalf("fresh cache should reuse the same resource record")
	}
	if filePath1 != filePath2 {
		t.Fatalf("fresh cache should reuse the same stored file path")
	}
}

func TestProcessResource_ETagRevalidationAvoidsBodyRedownload(t *testing.T) {
	dedup, db, fs := newFrameCaptureTestDeduplicator(t)
	defer db.Close()

	var totalHits atomic.Int32
	var notModifiedHits atomic.Int32
	const etag = `"style-v1"`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		totalHits.Add(1)
		w.Header().Set("ETag", etag)
		if r.Header.Get("If-None-Match") == etag {
			notModifiedHits.Add(1)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "text/css")
		_, _ = w.Write([]byte("body { color: red; }"))
	}))
	defer server.Close()

	baseURL := routeStorageHTTPClientToServer(t, fs, server)
	resourceURL := fmt.Sprintf("%s/etag-style.css?nonce=%d", baseURL, time.Now().UnixNano())
	pageURL := baseURL + "/page"

	resourceID1, filePath1, _, err := dedup.ProcessResource(resourceURL, "css", pageURL, nil, nil)
	if err != nil {
		t.Fatalf("first ProcessResource failed: %v", err)
	}
	resourceID2, filePath2, _, err := dedup.ProcessResource(resourceURL, "css", pageURL, nil, nil)
	if err != nil {
		t.Fatalf("second ProcessResource failed: %v", err)
	}

	if totalHits.Load() != 2 {
		t.Fatalf("expected one initial request and one validator recheck, got %d requests", totalHits.Load())
	}
	if notModifiedHits.Load() != 1 {
		t.Fatalf("expected second request to return 304 Not Modified")
	}
	if resourceID1 != resourceID2 {
		t.Fatalf("etag revalidation should reuse the same resource record")
	}
	if filePath1 != filePath2 {
		t.Fatalf("etag revalidation should reuse the same stored file path")
	}
}

func TestProcessResource_304NoCacheDoesNotRestoreOldFreshness(t *testing.T) {
	dedup, db, fs := newFrameCaptureTestDeduplicator(t)
	defer db.Close()

	var totalHits atomic.Int32
	var notModifiedHits atomic.Int32
	const etag = `"style-v1"`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		totalHits.Add(1)
		w.Header().Set("ETag", etag)
		if r.Header.Get("If-None-Match") == etag {
			notModifiedHits.Add(1)
			w.Header().Set("Cache-Control", "no-cache")
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "text/css")
		w.Header().Set("Cache-Control", "public, max-age=60")
		_, _ = w.Write([]byte("body { color: red; }"))
	}))
	defer server.Close()

	baseURL := routeStorageHTTPClientToServer(t, fs, server)
	resourceURL := fmt.Sprintf("%s/no-cache-304.css?nonce=%d", baseURL, time.Now().UnixNano())
	pageURL := baseURL + "/page"

	resourceID1, filePath1, _, err := dedup.ProcessResource(resourceURL, "css", pageURL, nil, nil)
	if err != nil {
		t.Fatalf("first ProcessResource failed: %v", err)
	}

	entry, ok := dedup.cache.Load(resourceURL)
	if !ok {
		t.Fatalf("expected cache entry for %s", resourceURL)
	}
	cached := entry.(*resourceCacheEntry)
	cached.freshUntil = time.Now().Add(-time.Second)
	cached.cachedAt = time.Now()

	resourceID2, filePath2, _, err := dedup.ProcessResource(resourceURL, "css", pageURL, nil, nil)
	if err != nil {
		t.Fatalf("second ProcessResource failed: %v", err)
	}

	entry, ok = dedup.cache.Load(resourceURL)
	if !ok {
		t.Fatalf("expected cache entry after 304 revalidation")
	}
	cached = entry.(*resourceCacheEntry)
	if !cached.freshUntil.IsZero() {
		t.Fatalf("304 with Cache-Control: no-cache should clear freshness window")
	}

	resourceID3, filePath3, _, err := dedup.ProcessResource(resourceURL, "css", pageURL, nil, nil)
	if err != nil {
		t.Fatalf("third ProcessResource failed: %v", err)
	}

	if totalHits.Load() != 3 {
		t.Fatalf("expected three upstream requests, got %d", totalHits.Load())
	}
	if notModifiedHits.Load() != 2 {
		t.Fatalf("expected both revalidations to return 304, got %d", notModifiedHits.Load())
	}
	if resourceID1 != resourceID2 || resourceID2 != resourceID3 {
		t.Fatalf("304 revalidation should keep reusing the same resource record")
	}
	if filePath1 != filePath2 || filePath2 != filePath3 {
		t.Fatalf("304 revalidation should keep reusing the same stored file path")
	}
}

func TestProcessResource_DownloadFailureDoesNotFallbackAcrossQueryVariants(t *testing.T) {
	dedup, db, fs := newFrameCaptureTestDeduplicator(t)
	defer db.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		_, _ = w.Write([]byte("body { color: red; }"))
	}))

	baseURL := routeStorageHTTPClientToServer(t, fs, server)
	pageURL := baseURL + "/page"
	resourceURL1 := baseURL + "/style.css?token=one"
	resourceURL2 := baseURL + "/style.css?token=two"

	if _, _, _, err := dedup.ProcessResource(resourceURL1, "css", pageURL, nil, nil); err != nil {
		t.Fatalf("ProcessResource(resourceURL1) failed: %v", err)
	}

	server.Close()

	if _, _, _, err := dedup.ProcessResource(resourceURL2, "css", pageURL, nil, nil); err == nil {
		t.Fatalf("expected download failure for query variant without unsafe fallback")
	}
}

func TestProcessResource_DownloadFailureFallsBackToExactURL(t *testing.T) {
	dedup, db, fs := newFrameCaptureTestDeduplicator(t)
	defer db.Close()
	cssToken := fmt.Sprintf("%d", time.Now().UnixNano())

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		_, _ = w.Write([]byte("body { color: red; } /* " + cssToken + " */"))
	}))

	baseURL := routeStorageHTTPClientToServer(t, fs, server)
	pageURL := baseURL + "/page"
	resourceURL := fmt.Sprintf("%s/style.css?token=%d", baseURL, time.Now().UnixNano())

	resourceID1, filePath1, _, err := dedup.ProcessResource(resourceURL, "css", pageURL, nil, nil)
	if err != nil {
		t.Fatalf("first ProcessResource failed: %v", err)
	}

	server.Close()

	resourceID2, filePath2, _, err := dedup.ProcessResource(resourceURL, "css", pageURL, nil, nil)
	if err != nil {
		t.Fatalf("expected exact URL fallback to succeed, got: %v", err)
	}
	if resourceID2 != resourceID1 {
		t.Fatalf("fallback should reuse exact URL resource ID, got %d want %d", resourceID2, resourceID1)
	}
	if filePath2 != filePath1 {
		t.Fatalf("fallback should reuse exact URL file path, got %q want %q", filePath2, filePath1)
	}
}

func TestProcessResource_LastModifiedRevalidationAvoidsBodyRedownload(t *testing.T) {
	dedup, db, fs := newFrameCaptureTestDeduplicator(t)
	defer db.Close()

	var totalHits atomic.Int32
	var notModifiedHits atomic.Int32
	const lastModified = "Mon, 02 Jan 2006 15:04:05 GMT"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		totalHits.Add(1)
		w.Header().Set("Last-Modified", lastModified)
		if r.Header.Get("If-Modified-Since") == lastModified {
			notModifiedHits.Add(1)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte("console.log('last-modified cache');"))
	}))
	defer server.Close()

	baseURL := routeStorageHTTPClientToServer(t, fs, server)
	resourceURL := fmt.Sprintf("%s/last-modified.js?nonce=%d", baseURL, time.Now().UnixNano())
	pageURL := baseURL + "/page"

	resourceID1, filePath1, _, err := dedup.ProcessResource(resourceURL, "js", pageURL, nil, nil)
	if err != nil {
		t.Fatalf("first ProcessResource failed: %v", err)
	}
	resourceID2, filePath2, _, err := dedup.ProcessResource(resourceURL, "js", pageURL, nil, nil)
	if err != nil {
		t.Fatalf("second ProcessResource failed: %v", err)
	}

	if totalHits.Load() != 2 {
		t.Fatalf("expected one initial request and one last-modified recheck, got %d requests", totalHits.Load())
	}
	if notModifiedHits.Load() != 1 {
		t.Fatalf("expected second request to return 304 Not Modified via If-Modified-Since")
	}
	if resourceID1 != resourceID2 {
		t.Fatalf("last-modified revalidation should reuse the same resource record")
	}
	if filePath1 != filePath2 {
		t.Fatalf("last-modified revalidation should reuse the same stored file path")
	}
}

func TestProcessResource_ExpiredFreshCacheTriggersRedownload(t *testing.T) {
	dedup, db, fs := newFrameCaptureTestDeduplicator(t)
	defer db.Close()

	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/css")
		w.Header().Set("Cache-Control", "public, max-age=60")
		_, _ = w.Write([]byte("body { color: red; }"))
	}))
	defer server.Close()

	baseURL := routeStorageHTTPClientToServer(t, fs, server)
	resourceURL := fmt.Sprintf("%s/expired-style.css?nonce=%d", baseURL, time.Now().UnixNano())
	pageURL := baseURL + "/page"

	resourceID1, filePath1, _, err := dedup.ProcessResource(resourceURL, "css", pageURL, nil, nil)
	if err != nil {
		t.Fatalf("first ProcessResource failed: %v", err)
	}

	entry, ok := dedup.cache.Load(resourceURL)
	if !ok {
		t.Fatalf("expected cache entry for %s", resourceURL)
	}
	cached := entry.(*resourceCacheEntry)
	cached.freshUntil = time.Now().Add(-time.Second)
	cached.cachedAt = time.Now()

	resourceID2, filePath2, _, err := dedup.ProcessResource(resourceURL, "css", pageURL, nil, nil)
	if err != nil {
		t.Fatalf("second ProcessResource failed: %v", err)
	}

	if hits.Load() != 2 {
		t.Fatalf("expired fresh cache should trigger a second upstream request, got %d", hits.Load())
	}
	if resourceID1 != resourceID2 {
		t.Fatalf("expired fresh cache with unchanged content should still reuse the same resource record")
	}
	if filePath1 != filePath2 {
		t.Fatalf("expired fresh cache with unchanged content should still reuse the same stored file path")
	}
}

func TestProcessCapture_RollsBackPageOnFinalizeFailure(t *testing.T) {
	dedup, db, fs := newFrameCaptureTestDeduplicator(t)
	defer db.Close()

	req := &models.CaptureRequest{
		URL:   fmt.Sprintf("https://rollback-create.example.com/%d", time.Now().UnixNano()),
		Title: "rollback create",
		HTML:  "<html><body>rollback create test</body></html>",
	}

	var tempHTMLPath string
	dedup.testBeforeCreateFinalize = func(pageID int64, htmlPath string, resourceIDs []int64) error {
		tempHTMLPath = htmlPath
		return errors.New("forced finalize failure")
	}

	pageID, action, err := dedup.ProcessCapture(req)
	if err == nil {
		t.Fatalf("ProcessCapture should fail when finalize hook returns error")
	}
	if pageID != 0 || action != "" {
		t.Fatalf("failed create should not return a persisted page result")
	}

	contentHash := hashCaptureContent(req.HTML, req.Frames)
	page, err := db.GetPageByURLAndHash(req.URL, contentHash)
	if err != nil {
		t.Fatalf("GetPageByURLAndHash failed: %v", err)
	}
	if page != nil {
		t.Fatalf("failed create should not leave a page row behind")
	}

	if tempHTMLPath == "" {
		t.Fatalf("expected finalize hook to capture the temporary HTML path")
	}
	if _, err := os.Stat(filepath.Join(fs.baseDir, tempHTMLPath)); !os.IsNotExist(err) {
		t.Fatalf("failed create should delete temporary HTML, got err=%v", err)
	}
}

func TestUpdateCapture_PreservesOldSnapshotOnCommitFailure(t *testing.T) {
	dedup, db, fs := newFrameCaptureTestDeduplicator(t)
	defer db.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/style-v1.css":
			w.Header().Set("Content-Type", "text/css")
			_, _ = w.Write([]byte("body { color: red; }"))
		case "/style-v2.css":
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
		Title: "before update failure",
		HTML:  `<html><head><link rel="stylesheet" href="` + baseURL + `/style-v1.css"></head><body>before update failure</body></html>`,
	}

	pageID, action, err := dedup.ProcessCapture(createReq)
	if err != nil {
		t.Fatalf("ProcessCapture failed: %v", err)
	}
	if action != models.ArchiveActionCreated {
		t.Fatalf("action = %q, want %q", action, models.ArchiveActionCreated)
	}
	defer db.DeletePage(pageID)

	beforePage, err := db.GetPageByID(fmt.Sprintf("%d", pageID))
	if err != nil {
		t.Fatalf("GetPageByID(before) failed: %v", err)
	}
	if beforePage == nil {
		t.Fatalf("expected page %d to exist", pageID)
	}
	beforeResources, err := db.GetResourcesByPageID(pageID)
	if err != nil {
		t.Fatalf("GetResourcesByPageID(before) failed: %v", err)
	}
	if len(beforeResources) != 1 {
		t.Fatalf("expected 1 linked resource before failed update, got %d", len(beforeResources))
	}

	updateReq := &models.CaptureRequest{
		URL:   pageURL,
		Title: "after update failure",
		HTML:  `<html><head><link rel="stylesheet" href="` + baseURL + `/style-v2.css"></head><body>after update failure</body></html>`,
	}

	var tempHTMLPath string
	dedup.testBeforeUpdateCommit = func(pageID int64, htmlPath string, resourceIDs []int64) error {
		tempHTMLPath = htmlPath
		return errors.New("forced update commit failure")
	}

	action, err = dedup.UpdateCapture(pageID, updateReq)
	if err == nil {
		t.Fatalf("UpdateCapture should fail when commit hook returns error")
	}
	if action != "" {
		t.Fatalf("failed update should not report a successful action")
	}

	afterPage, err := db.GetPageByID(fmt.Sprintf("%d", pageID))
	if err != nil {
		t.Fatalf("GetPageByID(after) failed: %v", err)
	}
	if afterPage == nil {
		t.Fatalf("page should still exist after failed update")
	}
	if afterPage.HTMLPath != beforePage.HTMLPath {
		t.Fatalf("failed update should keep old html_path: got %q want %q", afterPage.HTMLPath, beforePage.HTMLPath)
	}
	if afterPage.ContentHash != beforePage.ContentHash {
		t.Fatalf("failed update should keep old content_hash")
	}
	if afterPage.Title != beforePage.Title {
		t.Fatalf("failed update should keep old title: got %q want %q", afterPage.Title, beforePage.Title)
	}

	afterResources, err := db.GetResourcesByPageID(pageID)
	if err != nil {
		t.Fatalf("GetResourcesByPageID(after) failed: %v", err)
	}
	if len(afterResources) != len(beforeResources) {
		t.Fatalf("failed update should preserve old resource links: got %d want %d", len(afterResources), len(beforeResources))
	}
	if afterResources[0].ID != beforeResources[0].ID {
		t.Fatalf("failed update should keep the old linked resource")
	}

	if tempHTMLPath == "" {
		t.Fatalf("expected commit hook to capture the temporary HTML path")
	}
	if _, err := os.Stat(filepath.Join(fs.baseDir, tempHTMLPath)); !os.IsNotExist(err) {
		t.Fatalf("failed update should delete temporary HTML, got err=%v", err)
	}
}

func TestUpdateCapture_ClearsBodyTextWhenSnapshotBecomesEmpty(t *testing.T) {
	dedup, db, _ := newFrameCaptureTestDeduplicator(t)
	defer db.Close()

	pageURL := fmt.Sprintf("https://body-text-clear.example.com/page-%d", time.Now().UnixNano())
	createReq := &models.CaptureRequest{
		URL:   pageURL,
		Title: "before body text clear",
		HTML:  `<html><body>search term should disappear</body></html>`,
	}

	pageID, action, err := dedup.ProcessCapture(createReq)
	if err != nil {
		t.Fatalf("ProcessCapture failed: %v", err)
	}
	if action != models.ArchiveActionCreated {
		t.Fatalf("action = %q, want %q", action, models.ArchiveActionCreated)
	}
	defer db.DeletePage(pageID)

	matches, err := db.SearchPages("disappear", nil, nil, "")
	if err != nil {
		t.Fatalf("SearchPages(before update) failed: %v", err)
	}
	if len(matches) == 0 {
		t.Fatalf("expected body text to be searchable before update")
	}

	updateReq := &models.CaptureRequest{
		URL:   pageURL,
		Title: "after body text clear",
		HTML:  `<html><body><script>ignored</script></body></html>`,
	}

	action, err = dedup.UpdateCapture(pageID, updateReq)
	if err != nil {
		t.Fatalf("UpdateCapture failed: %v", err)
	}
	if action != models.ArchiveActionUpdated {
		t.Fatalf("action = %q, want %q", action, models.ArchiveActionUpdated)
	}

	matches, err = db.SearchPages("disappear", nil, nil, "")
	if err != nil {
		t.Fatalf("SearchPages(after update) failed: %v", err)
	}
	for _, match := range matches {
		if match.ID == pageID {
			t.Fatalf("updated page should no longer match cleared body text")
		}
	}
}
