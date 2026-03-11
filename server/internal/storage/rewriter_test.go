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
