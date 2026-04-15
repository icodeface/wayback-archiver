package api

import (
	"strings"
	"testing"
	"time"

	"wayback/internal/models"
)

func TestInjectArchiveHeader_EscapesHeaderLinkHref(t *testing.T) {
	page := &models.Page{
		ID:         1,
		URL:        `https://example.com/?q=" onclick="alert(1)"&x=1`,
		CapturedAt: time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC),
	}

	got := injectArchiveHeader(`<html><body><main>hello</main></body></html>`, page, nil, nil, 1, "nonce")

	if strings.Contains(got, `href="https://example.com/?q=" onclick=`) {
		t.Fatalf("header link href was not escaped: %s", got)
	}

	wantHref := `href="https://example.com/?q=&quot; onclick=&quot;alert(1)&quot;&amp;x=1"`
	if !strings.Contains(got, wantHref) {
		t.Fatalf("header link href missing escaped URL, want substring %q in %s", wantHref, got)
	}
}
