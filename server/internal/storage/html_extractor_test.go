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

func TestExtractResources_AllHrefVariantsExtracted(t *testing.T) {
	extractor := NewHTMLResourceExtractor()

	// GitHub uses data-base-href alongside href — only real href should be extracted
	html := `<html><head>
		<link rel="alternate icon" class="js-site-favicon" type="image/png" href="https://github.githubassets.com/favicons/favicon.png">
		<link rel="icon" class="js-site-favicon" type="image/svg+xml" href="https://github.githubassets.com/favicons/favicon.svg" data-base-href="https://github.githubassets.com/favicons/favicon">
	</head></html>`

	resources := extractor.ExtractResources(html, "https://github.com")

	foundSVG := false
	foundPNG := false
	for _, r := range resources {
		if r.URL == "https://github.githubassets.com/favicons/favicon.svg" {
			foundSVG = true
		}
		if r.URL == "https://github.githubassets.com/favicons/favicon.png" {
			foundPNG = true
		}
		if r.URL == "https://github.githubassets.com/favicons/favicon" {
			t.Error("Should not extract data-base-href value (URL without extension)")
		}
	}
	if !foundSVG {
		t.Error("Should extract href value (favicon.svg)")
	}
	if !foundPNG {
		t.Error("Should extract alternate icon href value (favicon.png)")
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

func TestExtractResources_IframeHTML(t *testing.T) {
	extractor := NewHTMLResourceExtractor()

	html := `<html><body><iframe src="https://example.com/embed/app.html?id=1"></iframe></body></html>`
	resources := extractor.ExtractResources(html, "https://example.com/page")

	found := false
	for _, r := range resources {
		if r.URL == "https://example.com/embed/app.html?id=1" {
			found = true
			if r.Type != "html" {
				t.Fatalf("iframe resource type = %q, want html", r.Type)
			}
		}
	}
	if !found {
		t.Fatal("should extract iframe HTML resource")
	}
}

func TestExtractResources_IframeCGIWithoutHTMLExtension(t *testing.T) {
	extractor := NewHTMLResourceExtractor()

	html := `<html><body><iframe src="https://ic2.qzone.qq.com/cgi-bin/feeds/feeds_html_module?g_iframeUser=1&refer=2"></iframe></body></html>`
	resources := extractor.ExtractResources(html, "https://user.qzone.qq.com/775146258")

	for _, r := range resources {
		if r.URL == "https://ic2.qzone.qq.com/cgi-bin/feeds/feeds_html_module?g_iframeUser=1&refer=2" {
			if r.Type != "html" {
				t.Fatalf("iframe CGI resource type = %q, want html", r.Type)
			}
			return
		}
	}

	t.Fatal("should extract iframe CGI resource as html")
}

func TestExtractResources_SkipsFragmentOnlyCSSURLs(t *testing.T) {
	extractor := NewHTMLResourceExtractor()

	html := `<html><body><svg><rect style="fill:url(#paint0_linear_0_3);mask:url(%23clip)"></rect></svg></body></html>`
	resources := extractor.ExtractResources(html, "https://example.com/page")

	for _, r := range resources {
		if r.URL == "https://example.com/page#paint0_linear_0_3" {
			t.Fatalf("should not extract same-document fragment URL, got %q", r.URL)
		}
		if r.URL == "https://example.com/%23clip" || r.URL == "https://example.com/page%23clip" {
			t.Fatalf("should not extract encoded same-document fragment URL, got %q", r.URL)
		}
		if r.URL == "#paint0_linear_0_3" {
			t.Fatalf("should not extract raw fragment URL, got %q", r.URL)
		}
		if r.URL == "%23clip" {
			t.Fatalf("should not extract raw encoded fragment URL, got %q", r.URL)
		}
	}
}

func TestExtractResources_SkipsFragmentOnlyQuotedCSSURLs(t *testing.T) {
	extractor := NewHTMLResourceExtractor()

	html := `<html><body><div style="filter:url(&quot;#paint0_linear_0_3&quot;)"></div></body></html>`
	resources := extractor.ExtractResources(html, "https://example.com/page")

	for _, r := range resources {
		if r.URL == "https://example.com/page#paint0_linear_0_3" {
			t.Fatalf("should not extract same-document quoted fragment URL, got %q", r.URL)
		}
		if r.URL == "#paint0_linear_0_3" {
			t.Fatalf("should not extract raw quoted fragment URL, got %q", r.URL)
		}
	}
}

func TestExtractResources_PreservesAssetURLsWithFragments(t *testing.T) {
	extractor := NewHTMLResourceExtractor()

	html := `<html><body><div style="mask:url(icons.svg#sprite);clip-path:url(icons.svg%23encoded-sprite)"></div></body></html>`
	resources := extractor.ExtractResources(html, "https://example.com/assets/page")

	foundPlainFragment := false
	foundEncodedFragment := false
	for _, r := range resources {
		switch r.URL {
		case "https://example.com/assets/icons.svg#sprite":
			foundPlainFragment = true
			if r.Type != "image" {
				t.Fatalf("asset URL with fragment type = %q, want image", r.Type)
			}
		case "https://example.com/assets/icons.svg%23encoded-sprite":
			foundEncodedFragment = true
			if r.Type != "image" {
				t.Fatalf("asset URL with encoded fragment type = %q, want image", r.Type)
			}
		}
	}

	if !foundPlainFragment || !foundEncodedFragment {
		t.Fatalf("should preserve asset URL with fragment suffix, got %#v", resources)
	}
}

func TestExtractResources_SkipsDataURLNestedFragmentReferences(t *testing.T) {
	extractor := NewHTMLResourceExtractor()

	html := `<html><body><div style="background-image:url(&quot;data:image/svg+xml,%3Csvg%3E%3Ccircle mask='url(%23clip)'/%3E%3C/svg%3E&quot;)"></div></body></html>`
	resources := extractor.ExtractResources(html, "https://example.com/page")

	if len(resources) != 0 {
		t.Fatalf("data URL internals should not be extracted as resources, got %#v", resources)
	}
}

func TestExtractResources_SkipsUnsupportedCSSURLSchemes(t *testing.T) {
	extractor := NewHTMLResourceExtractor()

	html := `<html><body><div style="background:url(BLOB:https://example.com/id);border-image:url(&quot; JAVASCRIPT:noop &quot;);mask:url(about:blank);list-style:url(mailto:a@example.com);background-image:url(/img.png)"></div></body></html>`
	resources := extractor.ExtractResources(html, "https://example.com/page")

	if len(resources) != 1 {
		t.Fatalf("expected only the downloadable image URL, got %#v", resources)
	}
	if resources[0].URL != "https://example.com/img.png" {
		t.Fatalf("expected /img.png to be preserved, got %#v", resources)
	}
}

func TestExtractResources_SkipsMalformedAbsoluteHostInCSSURL(t *testing.T) {
	extractor := NewHTMLResourceExtractor()

	// Mirrors a real typo Google ships in their translate CSS.
	html := `<html><body><div style="background-image:url(https://www&google.com/images/zippy_minus_sm.gif)"></div></body></html>`
	resources := extractor.ExtractResources(html, "https://example.com/page")

	for _, r := range resources {
		if r.URL == "https://www&google.com/images/zippy_minus_sm.gif" {
			t.Fatalf("malformed absolute host should be skipped, got %q", r.URL)
		}
	}
}

func TestParseSrcset(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "standard srcset with width descriptors",
			input:    "image-300.png 300w, image-600.png 600w, image-1200.png 1200w",
			expected: []string{"image-300.png", "image-600.png", "image-1200.png"},
		},
		{
			name:     "standard srcset with density descriptors",
			input:    "image.png 1x, image@2x.png 2x",
			expected: []string{"image.png", "image@2x.png"},
		},
		{
			name:     "URL with commas in query params (OSS image processing)",
			input:    "https://www.okx.com/cdn/icon/btc.png?x-oss-process=image/format,webp/resize,w_200,h_200,type_6/ignore-error,1",
			expected: []string{"https://www.okx.com/cdn/icon/btc.png?x-oss-process=image/format,webp/resize,w_200,h_200,type_6/ignore-error,1"},
		},
		{
			name:     "single URL without descriptor",
			input:    "https://example.com/image.png",
			expected: []string{"https://example.com/image.png"},
		},
		{
			name:     "empty srcset",
			input:    "",
			expected: nil,
		},
		{
			name:     "whitespace only",
			input:    "   ",
			expected: nil,
		},
		{
			name:     "URLs with descriptors and query params containing commas",
			input:    "https://cdn.example.com/img.png?process=format,webp 300w, https://cdn.example.com/img2.png?process=format,webp 600w",
			expected: []string{"https://cdn.example.com/img.png?process=format,webp", "https://cdn.example.com/img2.png?process=format,webp"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseSrcset(tt.input)
			if len(result) != len(tt.expected) {
				t.Fatalf("expected %d URLs, got %d: %v", len(tt.expected), len(result), result)
			}
			for i, url := range result {
				if url != tt.expected[i] {
					t.Errorf("URL[%d]: expected %q, got %q", i, tt.expected[i], url)
				}
			}
		})
	}
}
