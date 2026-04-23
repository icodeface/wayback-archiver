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

func TestInjectArchiveHeader_LocalizesSnapshotTimesInBrowser(t *testing.T) {
	page := &models.Page{
		ID:         1,
		URL:        "https://example.com/page",
		CapturedAt: time.Date(2026, 4, 15, 12, 34, 56, 0, time.UTC),
	}
	prev := &models.Page{ID: 2, FirstVisited: time.Date(2026, 4, 14, 1, 2, 3, 0, time.UTC)}
	next := &models.Page{ID: 3, FirstVisited: time.Date(2026, 4, 16, 4, 5, 6, 0, time.UTC)}

	got := injectArchiveHeader(`<html><body><main>hello</main></body></html>`, page, prev, next, 3, "nonce")

	for _, want := range []string{
		`<time class="wayback-local-time" data-format="full" datetime="2026-04-15T12:34:56Z">2026-04-15 12:34:56 UTC</time>`,
		`<time class="wayback-local-time" data-format="nav" datetime="2026-04-14T01:02:03Z">04-14 01:02 UTC</time>`,
		`<time class="wayback-local-time" data-format="nav" datetime="2026-04-16T04:05:06Z">04-16 04:05 UTC</time>`,
		`function localizeArchiveTimes()`,
		`data-time-title="true"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing localized time markup %q in %s", want, got)
		}
	}
}
