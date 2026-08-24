package storage

import (
	"sync/atomic"
	"testing"
	"time"

	"wayback/internal/models"
)

func TestConcurrencyManagerUsesConfiguredWorkerLimit(t *testing.T) {
	m := NewConcurrencyManager(3)
	if got := m.Workers(); got != 3 {
		t.Fatalf("workers = %d, want 3", got)
	}
	unlock := m.AcquireDownload()
	unlock()
}

func TestResourceCacheEvictsExpiredBeforeOldest(t *testing.T) {
	c := NewResourceCache(1)
	c.Store("expired", 1, sizedString(500*1024), downloadMetadata{})
	entry := c.Load("expired")
	entry.cachedAt = time.Now().Add(-resourceCacheTTL - time.Second)
	c.Store("fresh", 2, sizedString(400*1024), downloadMetadata{})
	c.Store("new", 3, sizedString(200*1024), downloadMetadata{})
	if c.Load("expired") != nil {
		t.Fatal("expired entry should be evicted")
	}
	if c.Load("fresh") == nil || c.Load("new") == nil {
		t.Fatal("fresh entries should remain")
	}
}

func TestHTMLRewriterExplicitInputOutput(t *testing.T) {
	var calls atomic.Int32
	r := NewHTMLRewriter(func(req HTMLRewriteRequest) (HTMLRewriteResult, error) {
		calls.Add(1)
		return HTMLRewriteResult{HTML: req.HTML + "!", ResourceIDs: []int64{req.PageID}}, nil
	})
	result, err := r.Rewrite(HTMLRewriteRequest{HTML: "<html>", BaseURL: "https://example.com", PageID: 42, Frames: []models.FrameCapture{{Key: "f"}}})
	if err != nil {
		t.Fatal(err)
	}
	if result.HTML != "<html>!" || len(result.ResourceIDs) != 1 || result.ResourceIDs[0] != 42 || calls.Load() != 1 {
		t.Fatalf("unexpected rewrite result: %+v", result)
	}
}

func TestPageArchiverDelegatesToDeduplicator(t *testing.T) {
	p := &PageArchiver{process: func(*models.CaptureRequest) (int64, string, error) { return 1, "created", nil }}
	id, action, err := p.Process(&models.CaptureRequest{})
	if err != nil || id != 1 || action != "created" {
		t.Fatalf("got %d/%s/%v", id, action, err)
	}
}
