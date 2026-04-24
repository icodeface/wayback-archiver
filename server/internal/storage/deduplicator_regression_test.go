package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"wayback/internal/config"
	"wayback/internal/database"
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

func newSQLiteIntegrityTestDeduplicator(t *testing.T) (*Deduplicator, database.Database, *FileStorage) {
	t.Helper()

	dataDir := t.TempDir()
	dbPath := filepath.Join(dataDir, "wayback.db")
	db, err := database.NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("NewSQLite failed: %v", err)
	}

	fs := NewFileStorage(dataDir)
	dedup := NewDeduplicator(db, fs, config.ResourceConfig{
		Workers:           2,
		MetadataCacheMB:   10,
		DownloadTimeout:   1,
		StreamThresholdKB: 2048,
	})

	return dedup, db, fs
}

func createStoredTestResource(t *testing.T, db database.Database, fs *FileStorage, resourceURL, resourceType string, content []byte) (int64, string, string) {
	t.Helper()

	hashBytes := sha256.Sum256(content)
	hash := hex.EncodeToString(hashBytes[:])
	relPath, err := fs.SaveResource(content, hash, resourceType)
	if err != nil {
		t.Fatalf("SaveResource failed: %v", err)
	}
	resourceID, err := db.CreateResource(resourceURL, hash, resourceType, relPath, int64(len(content)))
	if err != nil {
		t.Fatalf("CreateResource failed: %v", err)
	}
	return resourceID, relPath, hash
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

func TestProcessCapture_FreshCacheReuseUpdatesLastSeenOnFinalize(t *testing.T) {
	dedup, db, fs := newFrameCaptureTestDeduplicator(t)
	defer db.Close()

	cssToken := fmt.Sprintf("%d", time.Now().UnixNano())
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/css")
		w.Header().Set("Cache-Control", "public, max-age=60")
		_, _ = w.Write([]byte("body { color: red; } /* " + cssToken + " */"))
	}))
	defer server.Close()

	baseURL := routeStorageHTTPClientToServer(t, fs, server)
	cssURL := fmt.Sprintf("%s/style.css?nonce=%d", baseURL, time.Now().UnixNano())

	firstReq := &models.CaptureRequest{
		URL:   fmt.Sprintf("%s/page-a-%d", baseURL, time.Now().UnixNano()),
		Title: "first fresh cache page",
		HTML:  `<html><head><link rel="stylesheet" href="` + cssURL + `"></head><body>first</body></html>`,
	}

	pageID1, action, err := dedup.ProcessCapture(firstReq)
	if err != nil {
		t.Fatalf("first ProcessCapture failed: %v", err)
	}
	if action != models.ArchiveActionCreated {
		t.Fatalf("first action = %q, want %q", action, models.ArchiveActionCreated)
	}
	defer db.DeletePage(pageID1)

	resource, err := db.GetResourceByURL(cssURL)
	if err != nil {
		t.Fatalf("GetResourceByURL after first capture failed: %v", err)
	}
	if resource == nil {
		t.Fatalf("expected resource record for %s", cssURL)
	}
	beforeLastSeen := resource.LastSeen

	time.Sleep(20 * time.Millisecond)

	secondReq := &models.CaptureRequest{
		URL:   fmt.Sprintf("%s/page-b-%d", baseURL, time.Now().UnixNano()),
		Title: "second fresh cache page",
		HTML:  `<html><head><link rel="stylesheet" href="` + cssURL + `"></head><body>second</body></html>`,
	}

	pageID2, action, err := dedup.ProcessCapture(secondReq)
	if err != nil {
		t.Fatalf("second ProcessCapture failed: %v", err)
	}
	if action != models.ArchiveActionCreated {
		t.Fatalf("second action = %q, want %q", action, models.ArchiveActionCreated)
	}
	defer db.DeletePage(pageID2)

	if hits.Load() != 1 {
		t.Fatalf("fresh cache reuse should avoid a second upstream request, got %d", hits.Load())
	}

	resource, err = db.GetResourceByURL(cssURL)
	if err != nil {
		t.Fatalf("GetResourceByURL after second capture failed: %v", err)
	}
	if resource == nil {
		t.Fatalf("expected resource record after second capture")
	}
	if !resource.LastSeen.After(beforeLastSeen) {
		t.Fatalf("resource last_seen = %v, want after %v", resource.LastSeen, beforeLastSeen)
	}

	linked, err := db.GetResourcesByPageID(pageID2)
	if err != nil {
		t.Fatalf("GetResourcesByPageID(second page) failed: %v", err)
	}
	if len(linked) != 1 {
		t.Fatalf("expected 1 linked resource on second page, got %d", len(linked))
	}
	if linked[0].ID != resource.ID {
		t.Fatalf("second page linked resource ID = %d, want %d", linked[0].ID, resource.ID)
	}
}

func TestUpdateCapture_FreshCacheReuseUpdatesLastSeenOnCommit(t *testing.T) {
	dedup, db, fs := newFrameCaptureTestDeduplicator(t)
	defer db.Close()

	cssToken := fmt.Sprintf("%d", time.Now().UnixNano())
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/css")
		w.Header().Set("Cache-Control", "public, max-age=60")
		_, _ = w.Write([]byte("body { color: blue; } /* " + cssToken + " */"))
	}))
	defer server.Close()

	baseURL := routeStorageHTTPClientToServer(t, fs, server)
	cssURL := fmt.Sprintf("%s/style.css?nonce=%d", baseURL, time.Now().UnixNano())
	pageURL := fmt.Sprintf("%s/update-page-%d", baseURL, time.Now().UnixNano())

	createReq := &models.CaptureRequest{
		URL:   pageURL,
		Title: "before cached update",
		HTML:  `<html><head><link rel="stylesheet" href="` + cssURL + `"></head><body>before</body></html>`,
	}

	pageID, action, err := dedup.ProcessCapture(createReq)
	if err != nil {
		t.Fatalf("ProcessCapture failed: %v", err)
	}
	if action != models.ArchiveActionCreated {
		t.Fatalf("create action = %q, want %q", action, models.ArchiveActionCreated)
	}
	defer db.DeletePage(pageID)

	resource, err := db.GetResourceByURL(cssURL)
	if err != nil {
		t.Fatalf("GetResourceByURL before update failed: %v", err)
	}
	if resource == nil {
		t.Fatalf("expected resource record before update")
	}
	beforeLastSeen := resource.LastSeen

	time.Sleep(20 * time.Millisecond)

	updateReq := &models.CaptureRequest{
		URL:   pageURL,
		Title: "after cached update",
		HTML:  `<html><head><link rel="stylesheet" href="` + cssURL + `"></head><body>after</body></html>`,
	}

	action, err = dedup.UpdateCapture(pageID, updateReq)
	if err != nil {
		t.Fatalf("UpdateCapture failed: %v", err)
	}
	if action != models.ArchiveActionUpdated {
		t.Fatalf("update action = %q, want %q", action, models.ArchiveActionUpdated)
	}

	if hits.Load() != 1 {
		t.Fatalf("fresh cache reuse during update should avoid a second upstream request, got %d", hits.Load())
	}

	resource, err = db.GetResourceByURL(cssURL)
	if err != nil {
		t.Fatalf("GetResourceByURL after update failed: %v", err)
	}
	if resource == nil {
		t.Fatalf("expected resource record after update")
	}
	if !resource.LastSeen.After(beforeLastSeen) {
		t.Fatalf("resource last_seen = %v, want after %v", resource.LastSeen, beforeLastSeen)
	}

	linked, err := db.GetResourcesByPageID(pageID)
	if err != nil {
		t.Fatalf("GetResourcesByPageID after update failed: %v", err)
	}
	if len(linked) != 1 {
		t.Fatalf("expected 1 linked resource after update, got %d", len(linked))
	}
	if linked[0].ID != resource.ID {
		t.Fatalf("updated page linked resource ID = %d, want %d", linked[0].ID, resource.ID)
	}
}

func TestQuarantineCorruptedCSS_QuarantinesExistingBadCSS(t *testing.T) {
	dedup, db, fs := newSQLiteIntegrityTestDeduplicator(t)
	defer db.Close()

	resourceURL := fmt.Sprintf("https://integrity-test.example.com/style.css?nonce=%d", time.Now().UnixNano())
	originalCSS := []byte("body { color: red; }")
	resourceID, filePath, _ := createStoredTestResource(t, db, fs, resourceURL, "css", originalCSS)

	corruptedCSS := "body { background: url(/archive/resources/bad/file.bin); }"
	if err := fs.UpdateResource(filePath, []byte(corruptedCSS)); err != nil {
		t.Fatalf("UpdateResource(corrupt css) failed: %v", err)
	}

	summary, err := dedup.QuarantineCorruptedCSS(10)
	if err != nil {
		t.Fatalf("QuarantineCorruptedCSS failed: %v", err)
	}
	if summary.ScannedResources != 1 || summary.ScannedFiles != 1 {
		t.Fatalf("unexpected scan summary: %#v", summary)
	}
	if summary.CorruptedFiles != 1 || summary.QuarantinedFiles != 1 {
		t.Fatalf("unexpected quarantine summary: %#v", summary)
	}

	resource, err := db.GetResourceByID(resourceID)
	if err != nil {
		t.Fatalf("GetResourceByID failed: %v", err)
	}
	if resource == nil || !resource.IsQuarantined {
		t.Fatalf("expected corrupted CSS resource to be quarantined")
	}
	if !strings.Contains(resource.FilePath, "resources/quarantine/") {
		t.Fatalf("quarantined css path = %q, want resources/quarantine/...", resource.FilePath)
	}

	reusable, err := db.GetResourceByURL(resourceURL)
	if err != nil {
		t.Fatalf("GetResourceByURL(active) failed: %v", err)
	}
	if reusable != nil {
		t.Fatalf("quarantined CSS should be hidden from reusable lookups")
	}

	quarantinedCSS, err := fs.ReadResource(resource.FilePath)
	if err != nil {
		t.Fatalf("ReadResource(quarantined css) failed: %v", err)
	}
	if string(quarantinedCSS) != corruptedCSS {
		t.Fatalf("quarantined css = %q, want %q", string(quarantinedCSS), corruptedCSS)
	}
}

func TestQuarantineCorruptedCSS_MixedHealthyCorruptedAndMissingFiles(t *testing.T) {
	dedup, db, fs := newSQLiteIntegrityTestDeduplicator(t)
	defer db.Close()

	healthyURL := fmt.Sprintf("https://integrity-test.example.com/healthy.css?nonce=%d", time.Now().UnixNano())
	corruptedURL := fmt.Sprintf("https://integrity-test.example.com/corrupted.css?nonce=%d", time.Now().UnixNano())
	missingURL := fmt.Sprintf("https://integrity-test.example.com/missing.css?nonce=%d", time.Now().UnixNano())
	imageURL := fmt.Sprintf("https://integrity-test.example.com/logo.png?nonce=%d", time.Now().UnixNano())

	healthyID, _, _ := createStoredTestResource(t, db, fs, healthyURL, "css", []byte("body { color: green; }"))
	corruptedID, corruptedPath, _ := createStoredTestResource(t, db, fs, corruptedURL, "css", []byte("body { color: red; }"))
	missingID, missingPath, _ := createStoredTestResource(t, db, fs, missingURL, "css", []byte("body { color: blue; }"))
	imageID, imagePath, _ := createStoredTestResource(t, db, fs, imageURL, "image", []byte("image-bytes"))

	corruptedCSS := "body { background: url(/archive/resources/bad/file.bin); }"
	if err := fs.UpdateResource(corruptedPath, []byte(corruptedCSS)); err != nil {
		t.Fatalf("UpdateResource(corrupted css) failed: %v", err)
	}
	if err := os.Remove(filepath.Join(fs.baseDir, missingPath)); err != nil {
		t.Fatalf("Remove(missing css) failed: %v", err)
	}
	if err := fs.UpdateResource(imagePath, []byte("corrupted-image")); err != nil {
		t.Fatalf("UpdateResource(image) failed: %v", err)
	}

	summary, err := dedup.QuarantineCorruptedCSS(1)
	if err != nil {
		t.Fatalf("QuarantineCorruptedCSS failed: %v", err)
	}
	if summary.ResourceType != "css" {
		t.Fatalf("summary.ResourceType = %q, want css", summary.ResourceType)
	}
	if summary.ScannedResources != 3 {
		t.Fatalf("ScannedResources = %d, want 3", summary.ScannedResources)
	}
	if summary.ScannedFiles != 3 {
		t.Fatalf("ScannedFiles = %d, want 3", summary.ScannedFiles)
	}
	if summary.CorruptedFiles != 1 || summary.MissingFiles != 1 || summary.QuarantinedFiles != 2 {
		t.Fatalf("unexpected scan summary: %#v", summary)
	}

	healthyResource, err := db.GetResourceByID(healthyID)
	if err != nil {
		t.Fatalf("GetResourceByID(healthy) failed: %v", err)
	}
	if healthyResource == nil || healthyResource.IsQuarantined {
		t.Fatalf("healthy CSS should remain reusable")
	}

	corruptedResource, err := db.GetResourceByID(corruptedID)
	if err != nil {
		t.Fatalf("GetResourceByID(corrupted) failed: %v", err)
	}
	if corruptedResource == nil || !corruptedResource.IsQuarantined {
		t.Fatalf("corrupted CSS should be quarantined")
	}
	if !strings.Contains(corruptedResource.FilePath, "resources/quarantine/") {
		t.Fatalf("corrupted CSS path = %q, want resources/quarantine/...", corruptedResource.FilePath)
	}
	if !strings.Contains(corruptedResource.QuarantineReason, "hash mismatch") {
		t.Fatalf("corrupted CSS reason = %q, want hash mismatch", corruptedResource.QuarantineReason)
	}

	missingResource, err := db.GetResourceByID(missingID)
	if err != nil {
		t.Fatalf("GetResourceByID(missing) failed: %v", err)
	}
	if missingResource == nil || !missingResource.IsQuarantined {
		t.Fatalf("missing CSS should be quarantined")
	}
	if missingResource.FilePath != missingPath {
		t.Fatalf("missing CSS path = %q, want original path %q", missingResource.FilePath, missingPath)
	}
	if missingResource.QuarantineReason != "resource file missing" {
		t.Fatalf("missing CSS reason = %q, want resource file missing", missingResource.QuarantineReason)
	}

	imageResource, err := db.GetResourceByID(imageID)
	if err != nil {
		t.Fatalf("GetResourceByID(image) failed: %v", err)
	}
	if imageResource == nil || imageResource.IsQuarantined {
		t.Fatalf("non-CSS resources should not be scanned by QuarantineCorruptedCSS")
	}

	activeCorrupted, err := db.GetResourceByURL(corruptedURL)
	if err != nil {
		t.Fatalf("GetResourceByURL(corrupted) failed: %v", err)
	}
	if activeCorrupted != nil {
		t.Fatalf("corrupted CSS should be hidden from active lookups")
	}
	activeMissing, err := db.GetResourceByURL(missingURL)
	if err != nil {
		t.Fatalf("GetResourceByURL(missing) failed: %v", err)
	}
	if activeMissing != nil {
		t.Fatalf("missing CSS should be hidden from active lookups")
	}
	activeHealthy, err := db.GetResourceByURL(healthyURL)
	if err != nil {
		t.Fatalf("GetResourceByURL(healthy) failed: %v", err)
	}
	if activeHealthy == nil || activeHealthy.ID != healthyID {
		t.Fatalf("healthy CSS should remain available via active lookup")
	}
	activeImage, err := db.GetResourceByURL(imageURL)
	if err != nil {
		t.Fatalf("GetResourceByURL(image) failed: %v", err)
	}
	if activeImage == nil || activeImage.ID != imageID {
		t.Fatalf("non-CSS resource should remain available via active lookup")
	}

	quarantinedCSS, err := fs.ReadResource(corruptedResource.FilePath)
	if err != nil {
		t.Fatalf("ReadResource(quarantined corrupted css) failed: %v", err)
	}
	if string(quarantinedCSS) != corruptedCSS {
		t.Fatalf("quarantined corrupted css = %q, want %q", string(quarantinedCSS), corruptedCSS)
	}
}

func TestQuarantineCorruptedCSS_QuarantinesAllRowsSharingSameFilePath(t *testing.T) {
	dedup, db, fs := newSQLiteIntegrityTestDeduplicator(t)
	defer db.Close()

	sharedContent := []byte("body { color: red; }")
	urlA := fmt.Sprintf("https://integrity-test.example.com/shared-a.css?nonce=%d", time.Now().UnixNano())
	urlB := fmt.Sprintf("https://integrity-test.example.com/shared-b.css?nonce=%d", time.Now().UnixNano())

	resourceIDA, sharedPath, sharedHash := createStoredTestResource(t, db, fs, urlA, "css", sharedContent)
	resourceIDB, err := db.CreateResource(urlB, sharedHash, "css", sharedPath, int64(len(sharedContent)))
	if err != nil {
		t.Fatalf("CreateResource(shared duplicate) failed: %v", err)
	}

	corruptedCSS := "body { background: url(/archive/resources/shared/file.bin); }"
	if err := fs.UpdateResource(sharedPath, []byte(corruptedCSS)); err != nil {
		t.Fatalf("UpdateResource(shared css) failed: %v", err)
	}

	summary, err := dedup.QuarantineCorruptedCSS(10)
	if err != nil {
		t.Fatalf("QuarantineCorruptedCSS failed: %v", err)
	}
	if summary.ScannedResources != 2 {
		t.Fatalf("ScannedResources = %d, want 2", summary.ScannedResources)
	}
	if summary.ScannedFiles != 1 {
		t.Fatalf("ScannedFiles = %d, want 1", summary.ScannedFiles)
	}
	if summary.CorruptedFiles != 1 || summary.QuarantinedFiles != 1 {
		t.Fatalf("unexpected scan summary: %#v", summary)
	}

	resourceA, err := db.GetResourceByID(resourceIDA)
	if err != nil {
		t.Fatalf("GetResourceByID(A) failed: %v", err)
	}
	resourceB, err := db.GetResourceByID(resourceIDB)
	if err != nil {
		t.Fatalf("GetResourceByID(B) failed: %v", err)
	}
	if resourceA == nil || resourceB == nil || !resourceA.IsQuarantined || !resourceB.IsQuarantined {
		t.Fatalf("all rows sharing the same file path should be quarantined")
	}
	if resourceA.FilePath != resourceB.FilePath {
		t.Fatalf("shared quarantined path mismatch: %q vs %q", resourceA.FilePath, resourceB.FilePath)
	}
	if !strings.Contains(resourceA.FilePath, "resources/quarantine/") {
		t.Fatalf("quarantined shared path = %q, want resources/quarantine/...", resourceA.FilePath)
	}
}

func TestQuarantineCorruptedCSS_IsIdempotentOnRepeatedRuns(t *testing.T) {
	dedup, db, fs := newSQLiteIntegrityTestDeduplicator(t)
	defer db.Close()

	resourceURL := fmt.Sprintf("https://integrity-test.example.com/repeat.css?nonce=%d", time.Now().UnixNano())
	_, filePath, _ := createStoredTestResource(t, db, fs, resourceURL, "css", []byte("body { color: red; }"))
	if err := fs.UpdateResource(filePath, []byte("body { background: url(/archive/resources/repeat.bin); }")); err != nil {
		t.Fatalf("UpdateResource(repeat css) failed: %v", err)
	}

	first, err := dedup.QuarantineCorruptedCSS(10)
	if err != nil {
		t.Fatalf("first QuarantineCorruptedCSS failed: %v", err)
	}
	if first.QuarantinedFiles != 1 {
		t.Fatalf("first run QuarantinedFiles = %d, want 1", first.QuarantinedFiles)
	}

	second, err := dedup.QuarantineCorruptedCSS(10)
	if err != nil {
		t.Fatalf("second QuarantineCorruptedCSS failed: %v", err)
	}
	if second.ScannedResources != 0 || second.ScannedFiles != 0 || second.CorruptedFiles != 0 || second.MissingFiles != 0 || second.QuarantinedFiles != 0 {
		t.Fatalf("second run should be idempotent, got %#v", second)
	}
}

func TestProcessCapture_SkipsFragmentOnlyURLsDuringFinalize(t *testing.T) {
	dedup, db, fs := newFrameCaptureTestDeduplicator(t)
	defer db.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/style.css":
			w.Header().Set("Content-Type", "text/css")
			_, _ = w.Write([]byte(`.mask{filter:url(#goo)} .icon{background-image:url("icons.svg#sprite")}`))
		case "/icons.svg":
			w.Header().Set("Content-Type", "image/svg+xml")
			_, _ = w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><symbol id="sprite"></symbol></svg>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	baseURL := routeStorageHTTPClientToServer(t, fs, server)
	cssURL := fmt.Sprintf("%s/style.css?nonce=%d", baseURL, time.Now().UnixNano())
	expectedSpriteURL := baseURL + "/icons.svg#sprite"

	var requestedMu sync.Mutex
	requested := make([]string, 0, 2)
	dedup.testBeforeResourceCreate = func(url string) {
		requestedMu.Lock()
		requested = append(requested, url)
		requestedMu.Unlock()
	}

	req := &models.CaptureRequest{
		URL:   fmt.Sprintf("%s/page-%d", baseURL, time.Now().UnixNano()),
		Title: "fragment-only resources are skipped",
		HTML:  `<html><head><link rel="stylesheet" href="` + cssURL + `"></head><body><svg><rect style="fill:url(#paint0_linear_0_3)"></rect></svg></body></html>`,
	}

	pageID, action, err := dedup.ProcessCapture(req)
	if err != nil {
		t.Fatalf("ProcessCapture failed: %v", err)
	}
	if action != models.ArchiveActionCreated {
		t.Fatalf("action = %q, want %q", action, models.ArchiveActionCreated)
	}
	defer db.DeletePage(pageID)

	requestedMu.Lock()
	requestedCopy := append([]string(nil), requested...)
	requestedMu.Unlock()

	foundCSS := false
	foundSprite := false
	for _, resourceURL := range requestedCopy {
		if strings.Contains(resourceURL, "#paint0_linear_0_3") || strings.Contains(resourceURL, "#goo") {
			t.Fatalf("fragment-only URL should not be processed as a resource: %q", resourceURL)
		}
		if resourceURL == cssURL {
			foundCSS = true
		}
		if resourceURL == expectedSpriteURL {
			foundSprite = true
		}
	}
	if !foundCSS {
		t.Fatalf("expected stylesheet URL to be processed, got %#v", requestedCopy)
	}
	if !foundSprite {
		t.Fatalf("expected asset URL with fragment to be preserved, got %#v", requestedCopy)
	}

	linked, err := db.GetResourcesByPageID(pageID)
	if err != nil {
		t.Fatalf("GetResourcesByPageID failed: %v", err)
	}
	if len(linked) != 2 {
		t.Fatalf("expected 2 linked resources after skipping fragment-only URLs, got %d", len(linked))
	}

	linkedCSS := false
	linkedSprite := false
	for _, resource := range linked {
		if resource.URL == cssURL {
			linkedCSS = true
		}
		if resource.URL == expectedSpriteURL {
			linkedSprite = true
		}
		if strings.Contains(resource.URL, "#paint0_linear_0_3") || strings.HasSuffix(resource.URL, "#goo") {
			t.Fatalf("fragment-only URL should not be linked to the page: %q", resource.URL)
		}
	}
	if !linkedCSS || !linkedSprite {
		t.Fatalf("expected linked resources to contain stylesheet and sprite asset, got %#v", linked)
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
