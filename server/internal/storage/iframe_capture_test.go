package storage

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"wayback/internal/config"
	"wayback/internal/database"
	"wayback/internal/models"
)

func newFrameCaptureTestDeduplicator(t *testing.T) (*Deduplicator, *database.DB, *FileStorage) {
	t.Helper()

	user := os.Getenv("USER")
	if user == "" {
		user = "apple"
	}

	db, err := database.New("localhost", "5432", user, "", "wayback")
	if err != nil {
		t.Skip("PostgreSQL not available:", err)
	}
	skipIfNoDB(t, db)

	fs := NewFileStorage(t.TempDir())
	dedup := NewDeduplicator(db, fs, config.ResourceConfig{
		Workers:           2,
		MetadataCacheMB:   10,
		DownloadTimeout:   1,
		StreamThresholdKB: 2048,
	})

	return dedup, db, fs
}

func TestProcessCapture_UsesFrameSnapshotForTopLevelIframe(t *testing.T) {
	dedup, db, fs := newFrameCaptureTestDeduplicator(t)
	defer db.Close()

	nonce := strconv.FormatInt(time.Now().UnixNano(), 10)
	pageURL := "https://frame-top-level.example.com/page-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	frameURL := "https://frame-snapshot.invalid/embed/module.html?view=full&nonce=" + nonce
	frameKey := "frame-top-level"

	req := &models.CaptureRequest{
		URL:   pageURL,
		Title: "frame snapshot top level",
		HTML:  `<html><body><iframe data-wayback-frame-key="` + frameKey + `" src="` + frameURL + `"></iframe></body></html>`,
		Frames: []models.FrameCapture{{
			Key:   frameKey,
			URL:   frameURL,
			Title: "embedded module",
			HTML:  `<!DOCTYPE html><html><body><div id="frame-content">captured top-level iframe ` + nonce + `</div></body></html>`,
		}},
	}

	pageID, action, err := dedup.ProcessCapture(req)
	if err != nil {
		t.Fatalf("ProcessCapture failed: %v", err)
	}
	defer db.DeletePage(pageID)

	if action != models.ArchiveActionCreated {
		t.Fatalf("action = %q, want %q", action, models.ArchiveActionCreated)
	}

	resource, err := db.GetLinkedResourceByURLAndPageID(frameURL, pageID)
	if err != nil {
		t.Fatalf("GetLinkedResourceByURLAndPageID failed: %v", err)
	}
	if resource == nil {
		t.Fatalf("expected linked iframe resource for %s", frameURL)
	}
	if resource.ResourceType != "html" {
		t.Fatalf("iframe resource type = %q, want html", resource.ResourceType)
	}
	if !strings.HasSuffix(resource.FilePath, ".html") {
		t.Fatalf("iframe resource path = %q, want .html suffix", resource.FilePath)
	}

	resourceHTML, err := os.ReadFile(filepath.Join(fs.baseDir, resource.FilePath))
	if err != nil {
		t.Fatalf("ReadFile iframe resource failed: %v", err)
	}
	if !strings.Contains(string(resourceHTML), `captured top-level iframe `+nonce) {
		t.Fatalf("iframe resource should contain uploaded snapshot, got: %s", string(resourceHTML))
	}

	page, err := db.GetPageByID(strconv.FormatInt(pageID, 10))
	if err != nil {
		t.Fatalf("GetPageByID failed: %v", err)
	}
	if page == nil {
		t.Fatalf("expected page %d", pageID)
	}

	storedHTML, err := os.ReadFile(filepath.Join(fs.baseDir, page.HTMLPath))
	if err != nil {
		t.Fatalf("ReadFile page html failed: %v", err)
	}

	expectedProxyURL := archiveProxyURL(pageID, page.CapturedAt.Format("20060102150405"), frameURL)
	if !strings.Contains(string(storedHTML), expectedProxyURL) {
		t.Fatalf("stored page HTML should rewrite iframe src to archived proxy URL %q", expectedProxyURL)
	}
}

func TestProcessCapture_UsesFrameSnapshotForNestedIframe(t *testing.T) {
	dedup, db, fs := newFrameCaptureTestDeduplicator(t)
	defer db.Close()

	nonce := strconv.FormatInt(time.Now().UnixNano(), 10)
	pageURL := "https://frame-nested.example.com/page-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	outerURL := "https://frame-snapshot.invalid/embed/outer.html?nonce=" + nonce
	innerURL := "https://frame-snapshot.invalid/embed/inner.html?nonce=" + nonce

	req := &models.CaptureRequest{
		URL:   pageURL,
		Title: "frame snapshot nested",
		HTML:  `<html><body><iframe data-wayback-frame-key="outer-frame" src="` + outerURL + `"></iframe></body></html>`,
		Frames: []models.FrameCapture{
			{
				Key:   "outer-frame",
				URL:   outerURL,
				Title: "outer frame",
				HTML:  `<!DOCTYPE html><html><body><div>outer frame ` + nonce + `</div><iframe data-wayback-frame-key="inner-frame" src="` + innerURL + `"></iframe></body></html>`,
			},
			{
				Key:   "inner-frame",
				URL:   innerURL,
				Title: "inner frame",
				HTML:  `<!DOCTYPE html><html><body><div id="inner-frame-content">captured nested iframe ` + nonce + `</div></body></html>`,
			},
		},
	}

	pageID, action, err := dedup.ProcessCapture(req)
	if err != nil {
		t.Fatalf("ProcessCapture failed: %v", err)
	}
	defer db.DeletePage(pageID)

	if action != models.ArchiveActionCreated {
		t.Fatalf("action = %q, want %q", action, models.ArchiveActionCreated)
	}

	outerResource, err := db.GetLinkedResourceByURLAndPageID(outerURL, pageID)
	if err != nil {
		t.Fatalf("GetLinkedResourceByURLAndPageID outer failed: %v", err)
	}
	if outerResource == nil {
		t.Fatalf("expected linked outer iframe resource for %s", outerURL)
	}
	if outerResource.ResourceType != "html" {
		t.Fatalf("outer iframe resource type = %q, want html", outerResource.ResourceType)
	}

	innerResource, err := db.GetLinkedResourceByURLAndPageID(innerURL, pageID)
	if err != nil {
		t.Fatalf("GetLinkedResourceByURLAndPageID inner failed: %v", err)
	}
	if innerResource == nil {
		t.Fatalf("expected linked inner iframe resource for %s", innerURL)
	}
	if innerResource.ResourceType != "html" {
		t.Fatalf("inner iframe resource type = %q, want html", innerResource.ResourceType)
	}

	page, err := db.GetPageByID(strconv.FormatInt(pageID, 10))
	if err != nil {
		t.Fatalf("GetPageByID failed: %v", err)
	}
	if page == nil {
		t.Fatalf("expected page %d", pageID)
	}
	timestamp := page.CapturedAt.Format("20060102150405")

	outerHTML, err := os.ReadFile(filepath.Join(fs.baseDir, outerResource.FilePath))
	if err != nil {
		t.Fatalf("ReadFile outer iframe resource failed: %v", err)
	}
	if !strings.Contains(string(outerHTML), `outer frame `+nonce) {
		t.Fatalf("outer iframe resource should contain uploaded snapshot")
	}

	expectedInnerProxyURL := regexp.QuoteMeta(archiveProxyURL(pageID, timestamp, innerURL))
	matched, err := regexp.MatchString(`src=["']`+expectedInnerProxyURL+`["']`, string(outerHTML))
	if err != nil {
		t.Fatalf("regexp.MatchString failed: %v", err)
	}
	if !matched {
		t.Fatalf("outer iframe resource should rewrite nested iframe src to archived proxy URL")
	}

	innerHTML, err := os.ReadFile(filepath.Join(fs.baseDir, innerResource.FilePath))
	if err != nil {
		t.Fatalf("ReadFile inner iframe resource failed: %v", err)
	}
	if !strings.Contains(string(innerHTML), `captured nested iframe `+nonce) {
		t.Fatalf("inner iframe resource should contain uploaded snapshot")
	}
}
