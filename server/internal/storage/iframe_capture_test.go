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

func TestArchiveFrameCapture_SameURLDifferentFrameHTMLCreatesDistinctArchivedResources(t *testing.T) {
	dedup, db, fs := newFrameCaptureTestDeduplicator(t)
	defer db.Close()

	nonce := strconv.FormatInt(time.Now().UnixNano(), 10)
	frameURL := "https://frame-snapshot.invalid/embed/shared.html?nonce=" + nonce
	frameA := models.FrameCapture{
		Key:   "frame-a",
		URL:   frameURL,
		Title: "frame A",
		HTML:  `<!DOCTYPE html><html><body><div id="frame-a-content">captured frame A ` + nonce + `</div></body></html>`,
	}
	frameB := models.FrameCapture{
		Key:   "frame-b",
		URL:   frameURL,
		Title: "frame B",
		HTML:  `<!DOCTYPE html><html><body><div id="frame-b-content">captured frame B ` + nonce + `</div></body></html>`,
	}

	frameMap := buildFrameCaptureMap([]models.FrameCapture{frameA, frameB})
	var resourceIDs []int64
	seen := make(map[int64]struct{})
	visiting := make(map[string]bool)
	archived := make(map[string]processedInlineHTML)

	resourceIDA, filePathA, err := dedup.archiveFrameCapture(frameA, nil, nil, 1, "20260416120000", frameMap, &resourceIDs, seen, visiting, archived)
	if err != nil {
		t.Fatalf("archiveFrameCapture(frameA) failed: %v", err)
	}
	resourceIDB, filePathB, err := dedup.archiveFrameCapture(frameB, nil, nil, 1, "20260416120000", frameMap, &resourceIDs, seen, visiting, archived)
	if err != nil {
		t.Fatalf("archiveFrameCapture(frameB) failed: %v", err)
	}

	if resourceIDA == resourceIDB {
		t.Fatalf("same URL but different frame snapshots should create distinct archived resources")
	}
	if filePathA == filePathB {
		t.Fatalf("same URL but different frame snapshots should store distinct archived HTML files")
	}
	if len(resourceIDs) != 2 {
		t.Fatalf("same URL but different frame snapshots should append two resource IDs, got %d", len(resourceIDs))
	}

	frameAHTML, err := os.ReadFile(filepath.Join(fs.baseDir, filePathA))
	if err != nil {
		t.Fatalf("ReadFile frame A archived html failed: %v", err)
	}
	if !strings.Contains(string(frameAHTML), `captured frame A `+nonce) {
		t.Fatalf("frame A archived html should contain frame A snapshot, got: %s", string(frameAHTML))
	}

	frameBHTML, err := os.ReadFile(filepath.Join(fs.baseDir, filePathB))
	if err != nil {
		t.Fatalf("ReadFile frame B archived html failed: %v", err)
	}
	if !strings.Contains(string(frameBHTML), `captured frame B `+nonce) {
		t.Fatalf("frame B archived html should contain frame B snapshot, got: %s", string(frameBHTML))
	}
}

func TestRewriteCapturedHTML_SameURLDifferentFrameKeysArchivesBothSnapshots(t *testing.T) {
	dedup, db, fs := newFrameCaptureTestDeduplicator(t)
	defer db.Close()

	nonce := strconv.FormatInt(time.Now().UnixNano(), 10)
	frameURL := "https://frame-snapshot.invalid/embed/shared-page.html?nonce=" + nonce
	htmlContent := `<html><body><iframe data-wayback-frame-key="frame-a" src="` + frameURL + `"></iframe><iframe data-wayback-frame-key="frame-b" src="` + frameURL + `"></iframe></body></html>`
	frameMap := buildFrameCaptureMap([]models.FrameCapture{
		{
			Key:   "frame-a",
			URL:   frameURL,
			Title: "frame A",
			HTML:  `<!DOCTYPE html><html><body><div id="frame-a-content">rewritten frame A ` + nonce + `</div></body></html>`,
		},
		{
			Key:   "frame-b",
			URL:   frameURL,
			Title: "frame B",
			HTML:  `<!DOCTYPE html><html><body><div id="frame-b-content">rewritten frame B ` + nonce + `</div></body></html>`,
		},
	})

	var resourceIDs []int64
	seen := make(map[int64]struct{})
	rewrittenHTML, err := dedup.rewriteCapturedHTML(htmlContent, "https://frame-page.example.com/page-"+nonce, nil, nil, 1, "20260416121000", frameMap, &resourceIDs, seen, make(map[string]bool), make(map[string]processedInlineHTML))
	if err != nil {
		t.Fatalf("rewriteCapturedHTML failed: %v", err)
	}

	if len(resourceIDs) != 2 {
		t.Fatalf("expected two archived iframe resources for same-URL different-key frames, got %d", len(resourceIDs))
	}

	foundFrameA := false
	foundFrameB := false
	for _, resourceID := range resourceIDs {
		resource, err := db.GetResourceByID(resourceID)
		if err != nil {
			t.Fatalf("GetResourceByID(%d) failed: %v", resourceID, err)
		}
		if resource == nil {
			t.Fatalf("expected resource %d to exist", resourceID)
		}

		archivedHTML, err := os.ReadFile(filepath.Join(fs.baseDir, resource.FilePath))
		if err != nil {
			t.Fatalf("ReadFile archived iframe html failed: %v", err)
		}
		content := string(archivedHTML)
		if strings.Contains(content, `rewritten frame A `+nonce) {
			foundFrameA = true
		}
		if strings.Contains(content, `rewritten frame B `+nonce) {
			foundFrameB = true
		}
	}

	if !foundFrameA || !foundFrameB {
		t.Fatalf("expected archived iframe resources to preserve both frame snapshots; foundFrameA=%v foundFrameB=%v", foundFrameA, foundFrameB)
	}

	proxyURL := archiveProxyURL(1, "20260416121000", frameURL)
	if strings.Count(rewrittenHTML, proxyURL) != 2 {
		t.Fatalf("rewritten html should rewrite both iframe tags to archive proxy URL %q", proxyURL)
	}
}
