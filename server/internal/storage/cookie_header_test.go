package storage

import (
	"testing"
	"time"

	"wayback/internal/models"
)

func TestBuildCookieHeaderAt_FiltersByBrowserRules(t *testing.T) {
	now := time.Unix(1_710_000_000, 0)
	pageURL := "https://app.example.com/dashboard"
	resourceURL := "https://cdn.example.com/assets/app.css"

	header := buildCookieHeaderAt(resourceURL, pageURL, []models.CaptureCookie{
		{Name: "shared", Value: "1", Domain: ".example.com", Path: "/"},
		{Name: "asset", Value: "1", Domain: ".example.com", Path: "/assets"},
		{Name: "hostonly", Value: "1", Domain: "app.example.com", Path: "/", HostOnly: true},
		{Name: "wrongpath", Value: "1", Domain: ".example.com", Path: "/account"},
		{Name: "expired", Value: "1", Domain: ".example.com", Path: "/", ExpirationDate: float64(now.Unix() - 1)},
		{Name: "secure", Value: "1", Domain: ".example.com", Path: "/", Secure: true},
	}, now)

	if header != "asset=1; secure=1; shared=1" {
		t.Fatalf("cookie header = %q, want %q", header, "asset=1; secure=1; shared=1")
	}
}

func TestBuildCookieHeaderAt_RespectsSchemefulSameSite(t *testing.T) {
	now := time.Unix(1_710_000_000, 0)

	t.Run("blocks lax cookie on cross-scheme subresource", func(t *testing.T) {
		header := buildCookieHeaderAt(
			"https://cdn.example.com/app.js",
			"http://app.example.com/page",
			[]models.CaptureCookie{{Name: "lax", Value: "1", Domain: ".example.com", Path: "/", SameSite: "lax"}},
			now,
		)
		if header != "" {
			t.Fatalf("cookie header = %q, want empty header", header)
		}
	})

	t.Run("allows strict cookie on same-site subresource", func(t *testing.T) {
		header := buildCookieHeaderAt(
			"https://cdn.example.com/app.js",
			"https://app.example.com/page",
			[]models.CaptureCookie{{Name: "strict", Value: "1", Domain: ".example.com", Path: "/", SameSite: "strict"}},
			now,
		)
		if header != "strict=1" {
			t.Fatalf("cookie header = %q, want %q", header, "strict=1")
		}
	})
}

func TestBuildCookieHeaderAt_RespectsPartitionTopLevelSite(t *testing.T) {
	now := time.Unix(1_710_000_000, 0)
	header := buildCookieHeaderAt(
		"https://cdn.example.com/app.js",
		"https://app.example.com/page",
		[]models.CaptureCookie{
			{Name: "match", Value: "1", Domain: ".example.com", Path: "/", PartitionTopLevelSite: "https://example.com"},
			{Name: "mismatch", Value: "1", Domain: ".example.com", Path: "/", PartitionTopLevelSite: "https://other.com"},
		},
		now,
	)

	if header != "match=1" {
		t.Fatalf("cookie header = %q, want %q", header, "match=1")
	}
}
