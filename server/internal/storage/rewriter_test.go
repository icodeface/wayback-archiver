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

func TestRewriteHTML_DotSlashRelativePath(t *testing.T) {
	r := newTestRewriter(990, "20260313", map[string]string{
		"https://newshacker.me/style.css":  "resources/2c/09/hash.css",
		"https://newshacker.me/favicon.png": "resources/f9/22/hash.img",
		"https://newshacker.me/shared.js":   "resources/4d/ee/hash.js",
	})
	baseURL := "https://newshacker.me/"

	html := `<link rel="stylesheet" href="./style.css"><link rel="icon" href="./favicon.png"><script src="./shared.js"></script>`
	html = ResolveRelativeURLs(html, baseURL)
	result := r.RewriteHTML(html)

	if strings.Contains(result, `"./style.css"`) {
		t.Errorf("./style.css should be rewritten, got: %s", result)
	}
	if strings.Contains(result, `"./favicon.png"`) {
		t.Errorf("./favicon.png should be rewritten, got: %s", result)
	}
	if strings.Contains(result, `"./shared.js"`) {
		t.Errorf("./shared.js should be rewritten, got: %s", result)
	}
	if !strings.Contains(result, `/archive/990/20260313mp_/https://newshacker.me/style.css`) {
		t.Errorf("Expected rewritten style.css URL, got: %s", result)
	}
}

func TestRewriteHTML_BareRelativePath(t *testing.T) {
	// Bare relative paths without ./ prefix (e.g., SPA apps like Angular)
	r := newTestRewriter(1104, "20260313144244", map[string]string{
		"https://dash.3ue.co/zh-Hans/styles-V46MLXWF.css": "resources/74/33/hash.css",
		"https://dash.3ue.co/zh-Hans/main-QVKUS6BA.js":    "resources/87/4b/hash.js",
		"https://dash.3ue.co/zh-Hans/polyfills-TR5YYZNL.js": "resources/32/bc/hash.js",
	})
	baseURL := "https://dash.3ue.co/zh-Hans/"

	html := `<link rel="stylesheet" href="styles-V46MLXWF.css" media="all"><script src="main-QVKUS6BA.js" type="module"></script><link rel="modulepreload" href="polyfills-TR5YYZNL.js">`
	html = ResolveRelativeURLs(html, baseURL)
	result := r.RewriteHTML(html)

	if strings.Contains(result, `"styles-V46MLXWF.css"`) {
		t.Errorf("bare relative CSS should be rewritten, got: %s", result)
	}
	if strings.Contains(result, `"main-QVKUS6BA.js"`) {
		t.Errorf("bare relative JS should be rewritten, got: %s", result)
	}
	if strings.Contains(result, `"polyfills-TR5YYZNL.js"`) {
		t.Errorf("bare relative modulepreload should be rewritten, got: %s", result)
	}
	if !strings.Contains(result, `/archive/1104/20260313144244mp_/https://dash.3ue.co/zh-Hans/styles-V46MLXWF.css`) {
		t.Errorf("Expected rewritten CSS URL, got: %s", result)
	}
	if !strings.Contains(result, `/archive/1104/20260313144244mp_/https://dash.3ue.co/zh-Hans/main-QVKUS6BA.js`) {
		t.Errorf("Expected rewritten JS URL, got: %s", result)
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

// TestRewriteHTML_UnmappedAbsolutePaths 测试兜底逻辑：重写未映射的绝对路径
func TestRewriteHTML_UnmappedAbsolutePaths(t *testing.T) {
	r := newTestRewriter(48, "20260314043758", map[string]string{
		"https://v2ex.com/assets/c7e730da4976f02d5012a1303a2c9d7f929049c6-combo.css?t=1773462600": "resources/ab/cd/hash.css",
	})
	r.SetBaseURL("https://v2ex.com")

	// HTML 中包含一个未映射的绝对路径（时间戳不同）
	html := `<link rel="stylesheet" href="/assets/c7e730da4976f02d5012a1303a2c9d7f929049c6-combo.css?t=177346860">`
	result := r.RewriteHTML(html)

	// 应该被重写为归档路径（即使不在映射表中）
	expected := `/archive/48/20260314043758mp_/https://v2ex.com/assets/c7e730da4976f02d5012a1303a2c9d7f929049c6-combo.css?t=177346860`
	if !strings.Contains(result, expected) {
		t.Errorf("unmapped absolute path should be rewritten, expected: %s, got: %s", expected, result)
	}
}

// TestRewriteHTML_UnmappedAbsolutePathsInStyle 测试 style 标签中的未映射路径
func TestRewriteHTML_UnmappedAbsolutePathsInStyle(t *testing.T) {
	r := newTestRewriter(22, "20260314043758", map[string]string{})
	r.SetBaseURL("https://example.com")

	// style 标签中包含未映射的绝对路径
	html := `<style>body { background: url(/images/bg.png); }</style>`
	result := r.RewriteHTML(html)

	// 应该被重写为归档路径
	expected := `url("/archive/22/20260314043758mp_/https://example.com/images/bg.png")`
	if !strings.Contains(result, expected) {
		t.Errorf("unmapped absolute path in style should be rewritten, expected: %s, got: %s", expected, result)
	}
}

// TestRewriteHTML_UnmappedSingleQuote 测试单引号属性的未映射路径
func TestRewriteHTML_UnmappedSingleQuote(t *testing.T) {
	r := newTestRewriter(55, "20260314070000", map[string]string{})
	r.SetBaseURL("https://v2ex.com")

	// 单引号属性
	html := `<link rel='stylesheet' href='/assets/combo.css?t=123'>`
	result := r.RewriteHTML(html)

	expected := `href='/archive/55/20260314070000mp_/https://v2ex.com/assets/combo.css?t=123'`
	if !strings.Contains(result, expected) {
		t.Errorf("single-quoted unmapped path should be rewritten, expected: %s, got: %s", expected, result)
	}
}

// TestRewriteHTML_UnmappedProtocolRelative 测试协议相对 URL 的未映射路径
func TestRewriteHTML_UnmappedProtocolRelative(t *testing.T) {
	r := newTestRewriter(56, "20260314070100", map[string]string{})
	r.SetBaseURL("https://example.com")

	// 协议相对 URL（双引号）
	html := `<img src="//cdn.example.com/logo.png">`
	result := r.RewriteHTML(html)

	expected := `src="/archive/56/20260314070100mp_/https://cdn.example.com/logo.png"`
	if !strings.Contains(result, expected) {
		t.Errorf("protocol-relative unmapped URL should be rewritten, expected: %s, got: %s", expected, result)
	}

	// 协议相对 URL（单引号）
	html2 := `<script src='//cdn.example.com/app.js'></script>`
	result2 := r.RewriteHTML(html2)

	expected2 := `src='/archive/56/20260314070100mp_/https://cdn.example.com/app.js'`
	if !strings.Contains(result2, expected2) {
		t.Errorf("single-quoted protocol-relative URL should be rewritten, expected: %s, got: %s", expected2, result2)
	}
}

func TestResolveRelativeURLs(t *testing.T) {
	baseURL := "https://pub.sakana.ai/doc-to-lora/"

	tests := []struct {
		name     string
		html     string
		expected string
	}{
		{
			name:     "dot-slash subdir path",
			html:     `<img src="./figs/image.png">`,
			expected: `<img src="https://pub.sakana.ai/doc-to-lora/figs/image.png">`,
		},
		{
			name:     "bare subdir path",
			html:     `<link href="css/style.css">`,
			expected: `<link href="https://pub.sakana.ai/doc-to-lora/css/style.css">`,
		},
		{
			name:     "parent dir path",
			html:     `<img src="../other/file.png">`,
			expected: `<img src="https://pub.sakana.ai/other/file.png">`,
		},
		{
			name:     "bare filename",
			html:     `<img src="image.png">`,
			expected: `<img src="https://pub.sakana.ai/doc-to-lora/image.png">`,
		},
		{
			name:     "absolute URL unchanged",
			html:     `<img src="https://cdn.example.com/img.png">`,
			expected: `<img src="https://cdn.example.com/img.png">`,
		},
		{
			name:     "absolute path unchanged",
			html:     `<img src="/assets/img.png">`,
			expected: `<img src="/assets/img.png">`,
		},
		{
			name:     "data URI unchanged",
			html:     `<img src="data:image/png;base64,abc">`,
			expected: `<img src="data:image/png;base64,abc">`,
		},
		{
			name:     "single-quoted attribute",
			html:     `<img src='./figs/image.png'>`,
			expected: `<img src='https://pub.sakana.ai/doc-to-lora/figs/image.png'>`,
		},
		{
			name:     "url() in CSS",
			html:     `url("../fonts/font.woff")`,
			expected: `url("https://pub.sakana.ai/fonts/font.woff")`,
		},
		{
			name:     "query string with &amp;",
			html:     `<img src="api/data?a=1&amp;b=2">`,
			expected: `<img src="https://pub.sakana.ai/doc-to-lora/api/data?a=1&b=2">`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ResolveRelativeURLs(tt.html, baseURL)
			if result != tt.expected {
				t.Errorf("\nexpected: %s\n     got: %s", tt.expected, result)
			}
		})
	}
}

func TestRewriteHTML_SubdirRelativePath(t *testing.T) {
	r := newTestRewriter(1713, "20260316102920", map[string]string{
		"https://pub.sakana.ai/doc-to-lora/figs/image.png":     "resources/ab/cd/hash1.img",
		"https://pub.sakana.ai/doc-to-lora/css/style.css":      "resources/ab/cd/hash2.css",
		"https://pub.sakana.ai/doc-to-lora/lib/template.v1.js": "resources/ab/cd/hash3.js",
	})
	r.SetBaseURL("https://pub.sakana.ai/doc-to-lora/")
	baseURL := "https://pub.sakana.ai/doc-to-lora/"
	archivePrefix := "/archive/1713/20260316102920mp_/"

	// 模拟完整流程：先 ResolveRelativeURLs，再 RewriteHTML
	html := `<img src="./figs/image.png"> <link href="./css/style.css"> <script src="./lib/template.v1.js"></script>`
	html = ResolveRelativeURLs(html, baseURL)
	result := r.RewriteHTML(html)

	if !strings.Contains(result, `src="`+archivePrefix+`https://pub.sakana.ai/doc-to-lora/figs/image.png"`) {
		t.Errorf("./figs/image.png should be rewritten, got: %s", result)
	}
	if !strings.Contains(result, `href="`+archivePrefix+`https://pub.sakana.ai/doc-to-lora/css/style.css"`) {
		t.Errorf("./css/style.css should be rewritten, got: %s", result)
	}
	if !strings.Contains(result, `src="`+archivePrefix+`https://pub.sakana.ai/doc-to-lora/lib/template.v1.js"`) {
		t.Errorf("./lib/template.v1.js should be rewritten, got: %s", result)
	}

	// ../path 形式
	html2 := ResolveRelativeURLs(`<img src="../other/file.png">`, baseURL)
	// ../other/file.png 解析为 https://pub.sakana.ai/other/file.png，不在映射中，不会被 rewriter 替换
	if !strings.Contains(html2, `src="https://pub.sakana.ai/other/file.png"`) {
		t.Errorf("../other/file.png should be resolved to absolute URL, got: %s", html2)
	}
}

