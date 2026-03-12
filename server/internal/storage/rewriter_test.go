package storage

import (
	"strings"
	"testing"
)

func newTestRewriter(pageID int64, timestamp string, mappings map[string]string) *URLRewriter {
	r := NewURLRewriter()
	r.SetPageID(pageID)
	r.SetTimestamp(timestamp)
	for url, path := range mappings {
		r.AddMapping(url, path)
	}
	return r
}

func TestRewriteHTML_DataSrcPreserved(t *testing.T) {
	r := newTestRewriter(1, "20260310", map[string]string{
		"https://example.com/real.jpg": "resources/ab/cd/hash.img",
	})

	html := `<img data-src="https://example.com/real.jpg" src="https://example.com/real.jpg">`
	result := r.RewriteHTML(html)

	// data-src should NOT be rewritten
	if !strings.Contains(result, `data-src="https://example.com/real.jpg"`) {
		t.Errorf("data-src should be preserved, got: %s", result)
	}
	// src should be rewritten
	if !strings.Contains(result, `src="/archive/1/20260310mp_/https://example.com/real.jpg"`) {
		t.Errorf("src should be rewritten, got: %s", result)
	}
}

func TestRewriteHTML_PosterAttribute(t *testing.T) {
	r := newTestRewriter(1, "20260310", map[string]string{
		"https://example.com/thumb.jpg": "resources/ab/cd/hash.img",
	})

	html := `<video src="other.mp4" poster="https://example.com/thumb.jpg" autoplay>`
	result := r.RewriteHTML(html)

	if !strings.Contains(result, `poster="/archive/1/20260310mp_/https://example.com/thumb.jpg"`) {
		t.Errorf("poster should be rewritten, got: %s", result)
	}
}

func TestRewriteHTML_HTMLEntityAmp(t *testing.T) {
	r := newTestRewriter(1, "20260310", map[string]string{
		"https://example.com/img.jpg?a=1&b=2": "resources/ab/cd/hash.img",
	})

	// HTML contains &amp; encoded version
	html := `<img src="https://example.com/img.jpg?a=1&amp;b=2">`
	result := r.RewriteHTML(html)

	if !strings.Contains(result, `src="/archive/1/20260310mp_/https://example.com/img.jpg?a=1&b=2"`) {
		t.Errorf("&amp; encoded URL should be rewritten to local path, got: %s", result)
	}
}

func TestRewriteHTML_ProtocolRelativeWithAmp(t *testing.T) {
	r := newTestRewriter(1, "20260310", map[string]string{
		"https://f.video.com/v.mp4?a=1&b=2": "resources/ab/cd/hash.bin",
	})

	// Protocol-relative + &amp; combo
	html := `<video src="//f.video.com/v.mp4?a=1&amp;b=2">`
	result := r.RewriteHTML(html)

	if !strings.Contains(result, `src="/archive/1/20260310mp_/https://f.video.com/v.mp4?a=1&b=2"`) {
		t.Errorf("Protocol-relative + &amp; URL should be rewritten, got: %s", result)
	}
}

func TestRewriteHTML_URLQuotEncoding(t *testing.T) {
	r := newTestRewriter(1, "20260310", map[string]string{
		"https://example.com/bg.jpg": "resources/ab/cd/hash.img",
	})

	html := `<div style="background-image: url(&quot;https://example.com/bg.jpg&quot;);">`
	result := r.RewriteHTML(html)

	if !strings.Contains(result, `url(&quot;/archive/1/20260310mp_/https://example.com/bg.jpg&quot;)`) {
		t.Errorf("url(&quot;...&quot;) should be rewritten, got: %s", result)
	}
}

func TestNormalizeHTMLURLs_DotDotPaths(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "href with ../ double quote",
			input:    `<link rel="stylesheet" href="https://example.com/a/b/../c/style.css">`,
			expected: `<link rel="stylesheet" href="https://example.com/a/c/style.css">`,
		},
		{
			name:     "href with ../ single quote",
			input:    `<link href='https://example.com/a/b/../c/style.css' rel="stylesheet">`,
			expected: `<link href='https://example.com/a/c/style.css' rel="stylesheet">`,
		},
		{
			name:     "src with ../",
			input:    `<script src="https://example.com/js/lib/../common/app.js"></script>`,
			expected: `<script src="https://example.com/js/common/app.js"></script>`,
		},
		{
			name:     "multiple ../ segments",
			input:    `<link href="https://example.com/a/b/c/../../d/style.css" rel="stylesheet">`,
			expected: `<link href="https://example.com/a/d/style.css" rel="stylesheet">`,
		},
		{
			name:     "preserves query params",
			input:    `<script src="https://example.com/a/../b/app.js?v=123"></script>`,
			expected: `<script src="https://example.com/b/app.js?v=123"></script>`,
		},
		{
			name:     "no ../ unchanged",
			input:    `<link href="https://example.com/normal/style.css" rel="stylesheet">`,
			expected: `<link href="https://example.com/normal/style.css" rel="stylesheet">`,
		},
		{
			name:     "multiple links mixed",
			input:    `<link href="https://a.com/x/../y.css" rel="stylesheet"><link href="https://b.com/normal.css" rel="stylesheet">`,
			expected: `<link href="https://a.com/y.css" rel="stylesheet"><link href="https://b.com/normal.css" rel="stylesheet">`,
		},
		{
			name:     "real world okx pattern",
			input:    `<link rel="stylesheet" type="text/css" href="https://www.okx.com/cdn/assets/okfe/okx-nav/header/../common/9214.466d2d42.css">`,
			expected: `<link rel="stylesheet" type="text/css" href="https://www.okx.com/cdn/assets/okfe/okx-nav/common/9214.466d2d42.css">`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeHTMLURLs(tt.input)
			if result != tt.expected {
				t.Errorf("NormalizeHTMLURLs()\ngot:  %s\nwant: %s", result, tt.expected)
			}
		})
	}
}

func TestRewriteHTML_NormalSrcHref(t *testing.T) {
	r := newTestRewriter(1, "20260310", map[string]string{
		"https://example.com/style.css": "resources/ab/cd/hash.css",
		"https://example.com/img.png":   "resources/ef/gh/hash.img",
	})

	html := `<link href="https://example.com/style.css" rel="stylesheet"><img src="https://example.com/img.png">`
	result := r.RewriteHTML(html)

	if !strings.Contains(result, `href="/archive/1/20260310mp_/https://example.com/style.css"`) {
		t.Errorf("href should be rewritten, got: %s", result)
	}
	if !strings.Contains(result, `src="/archive/1/20260310mp_/https://example.com/img.png"`) {
		t.Errorf("src should be rewritten, got: %s", result)
	}
}

func TestRewriteHTML_RelativePathWithAmpEncoding(t *testing.T) {
	// Next.js image optimization URLs: relative path with &amp; in HTML
	r := newTestRewriter(631, "20260312101010", map[string]string{
		"https://www.moltbook.com/_next/image?url=%2Fmoltbook-transparent.png&w=384&q=75": "resources/ab/cd/hash.img",
		"https://www.moltbook.com/_next/image?url=%2Fmoltbook-transparent.png&w=64&q=75":  "resources/ab/cd/hash2.img",
	})

	// HTML has relative path with &amp; encoding
	html := `<img src="/_next/image?url=%2Fmoltbook-transparent.png&amp;w=384&amp;q=75">`
	result := r.RewriteHTML(html)

	expected := `src="/archive/631/20260312101010mp_/https://www.moltbook.com/_next/image?url=%2Fmoltbook-transparent.png&w=384&q=75"`
	if !strings.Contains(result, expected) {
		t.Errorf("relative path with &amp; should be rewritten to archive path, got: %s", result)
	}
}

func TestRewriteHTML_MultiValueSrcset(t *testing.T) {
	r := newTestRewriter(631, "20260312101010", map[string]string{
		"https://www.moltbook.com/_next/image?url=%2Fmoltbook-transparent.png&w=256&q=75": "resources/ab/cd/hash1.img",
		"https://www.moltbook.com/_next/image?url=%2Fmoltbook-transparent.png&w=384&q=75": "resources/ab/cd/hash2.img",
	})

	// Multi-value srcset with &amp; encoding and descriptors
	html := `<link rel="preload" as="image" imagesrcset="/_next/image?url=%2Fmoltbook-transparent.png&amp;w=256&amp;q=75 1x, /_next/image?url=%2Fmoltbook-transparent.png&amp;w=384&amp;q=75 2x">`
	result := r.RewriteHTML(html)

	if !strings.Contains(result, `/archive/631/20260312101010mp_/https://www.moltbook.com/_next/image`) {
		t.Errorf("multi-value srcset URLs should be rewritten to archive paths, got: %s", result)
	}
	if !strings.Contains(result, `1x`) || !strings.Contains(result, `2x`) {
		t.Errorf("srcset descriptors should be preserved, got: %s", result)
	}
	// Should not contain unrewritten relative paths
	if strings.Contains(result, `"/_next/image`) {
		t.Errorf("should not contain unrewritten relative paths, got: %s", result)
	}
}

func TestRewriteHTML_MultiValueSrcsetOnImg(t *testing.T) {
	r := newTestRewriter(631, "20260312101010", map[string]string{
		"https://www.moltbook.com/_next/image?url=%2Flogo.png&w=32&q=75": "resources/ab/cd/hash1.img",
		"https://www.moltbook.com/_next/image?url=%2Flogo.png&w=64&q=75": "resources/ab/cd/hash2.img",
	})

	html := `<img srcset="/_next/image?url=%2Flogo.png&amp;w=32&amp;q=75 1x, /_next/image?url=%2Flogo.png&amp;w=64&amp;q=75 2x" src="/_next/image?url=%2Flogo.png&amp;w=64&amp;q=75">`
	result := r.RewriteHTML(html)

	// src should be rewritten
	if !strings.Contains(result, `src="/archive/631/20260312101010mp_/`) {
		t.Errorf("src should be rewritten to archive path, got: %s", result)
	}
	// srcset should be rewritten
	if !strings.Contains(result, `srcset="/archive/631/20260312101010mp_/`) {
		t.Errorf("srcset should be rewritten to archive path, got: %s", result)
	}
	// Should not contain unrewritten relative paths
	if strings.Contains(result, `"/_next/image`) || strings.Contains(result, ` /_next/image`) {
		t.Errorf("should not contain unrewritten relative paths, got: %s", result)
	}
}
