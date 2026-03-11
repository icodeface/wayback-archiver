package storage

import (
	"testing"
)

func TestExtractResources_DataSrcNotMatched(t *testing.T) {
	extractor := NewHTMLResourceExtractor()

	// Weibo pattern: data-src before src — extractor should capture src, not data-src
	html := `<html><body>
		<img data-src="https://weibo.com/false" src="https://tvax4.sinaimg.cn/crop.0.0.750.750.1024/avatar.jpg?KID=imgbed" class="woo-avatar-img">
	</body></html>`

	resources := extractor.ExtractResources(html, "https://weibo.com/u/123")

	found := false
	for _, r := range resources {
		if r.URL == "https://weibo.com/false" {
			t.Error("Should NOT extract data-src URL 'https://weibo.com/false'")
		}
		if r.URL == "https://tvax4.sinaimg.cn/crop.0.0.750.750.1024/avatar.jpg?KID=imgbed" {
			found = true
			if r.Type != "image" {
				t.Errorf("Expected type 'image', got '%s'", r.Type)
			}
		}
	}
	if !found {
		t.Error("Should extract real src URL 'https://tvax4.sinaimg.cn/crop.0.0.750.750.1024/avatar.jpg?KID=imgbed'")
	}
}

func TestExtractResources_DataSrcAfterSrc(t *testing.T) {
	extractor := NewHTMLResourceExtractor()

	// src before data-src — should still work
	html := `<html><body>
		<img src="https://example.com/real.jpg" data-src="https://example.com/lazy.jpg" class="img">
	</body></html>`

	resources := extractor.ExtractResources(html, "https://example.com")

	found := false
	for _, r := range resources {
		if r.URL == "https://example.com/real.jpg" {
			found = true
		}
	}
	if !found {
		t.Error("Should extract src URL when src comes before data-src")
	}
}

func TestExtractResources_OnlyDataSrc(t *testing.T) {
	extractor := NewHTMLResourceExtractor()

	// Only data-src, no src — should not match
	html := `<html><body>
		<img data-src="https://example.com/lazy.jpg" class="img">
	</body></html>`

	resources := extractor.ExtractResources(html, "https://example.com")

	for _, r := range resources {
		if r.URL == "https://example.com/lazy.jpg" {
			t.Error("Should NOT extract data-src when there is no src attribute")
		}
	}
}

func TestExtractResources_HTMLEntityDecode(t *testing.T) {
	extractor := NewHTMLResourceExtractor()

	// URL with &amp; in HTML — should be decoded to &
	html := `<html><body>
		<img src="https://example.com/img.jpg?a=1&amp;b=2">
	</body></html>`

	resources := extractor.ExtractResources(html, "https://example.com")

	found := false
	for _, r := range resources {
		if r.URL == "https://example.com/img.jpg?a=1&b=2" {
			found = true
		}
		if r.URL == "https://example.com/img.jpg?a=1&amp;b=2" {
			t.Error("URL should be HTML-unescaped, but got &amp; version")
		}
	}
	if !found {
		t.Error("Should extract URL with decoded &amp; -> &")
	}
}

func TestExtractResources_DotDotURLNormalized(t *testing.T) {
	extractor := NewHTMLResourceExtractor()

	// CSS link with ../ in absolute URL — should be normalized
	html := `<html><head>
		<link rel="stylesheet" href="https://example.com/assets/nav/header/../common/style.css">
		<link rel="stylesheet" href="https://example.com/assets/normal/style.css">
	</head></html>`

	resources := extractor.ExtractResources(html, "https://example.com/page")

	foundNormalized := false
	foundNormal := false
	for _, r := range resources {
		if r.URL == "https://example.com/assets/nav/common/style.css" {
			foundNormalized = true
			if r.Type != "css" {
				t.Errorf("Expected type 'css', got '%s'", r.Type)
			}
		}
		if r.URL == "https://example.com/assets/nav/header/../common/style.css" {
			t.Error("URL with ../ should be normalized, but got raw URL")
		}
		if r.URL == "https://example.com/assets/normal/style.css" {
			foundNormal = true
		}
	}
	if !foundNormalized {
		t.Error("Should extract and normalize URL with ../ to 'https://example.com/assets/nav/common/style.css'")
	}
	if !foundNormal {
		t.Error("Should extract normal URL 'https://example.com/assets/normal/style.css'")
	}
}

func TestExtractResources_VideoPoster(t *testing.T) {
	extractor := NewHTMLResourceExtractor()

	html := `<html><body>
		<video src="https://example.com/video.mov" poster="https://example.com/thumb.jpg" autoplay></video>
	</body></html>`

	resources := extractor.ExtractResources(html, "https://example.com")

	foundVideo := false
	foundPoster := false
	for _, r := range resources {
		if r.URL == "https://example.com/video.mov" {
			foundVideo = true
		}
		if r.URL == "https://example.com/thumb.jpg" {
			foundPoster = true
			if r.Type != "image" {
				t.Errorf("Poster should be type 'image', got '%s'", r.Type)
			}
		}
	}
	if !foundVideo {
		t.Error("Should extract video src")
	}
	if !foundPoster {
		t.Error("Should extract video poster")
	}
}
