package storage

import (
	"testing"
)

// TestNormalizeHTMLURLs_Precompiled 验证预编译正则版本的 NormalizeHTMLURLs 行为一致
func TestNormalizeHTMLURLs_Precompiled(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			"double dot path",
			`<link href="https://example.com/css/../style.css">`,
			`<link href="https://example.com/style.css">`,
		},
		{
			"no dot path unchanged",
			`<img src="https://example.com/image.png">`,
			`<img src="https://example.com/image.png">`,
		},
		{
			"single quote",
			`<script src='https://example.com/a/../b.js'>`,
			`<script src='https://example.com/b.js'>`,
		},
		{
			"poster attribute",
			`<video poster="https://example.com/a/b/../thumb.jpg">`,
			`<video poster="https://example.com/a/thumb.jpg">`,
		},
		{
			"multiple dot segments",
			`<a href="https://example.com/a/b/c/../../d.html">link</a>`,
			`<a href="https://example.com/a/d.html">link</a>`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeHTMLURLs(tt.input)
			if result != tt.want {
				t.Errorf("got %q, want %q", result, tt.want)
			}
		})
	}
}

// TestResolveRelativeURLs_Precompiled 验证预编译正则版本的 ResolveRelativeURLs 行为一致
func TestResolveRelativeURLs_Precompiled(t *testing.T) {
	baseURL := "https://example.com/blog/post.html"

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			"relative src DQ",
			`<img src="images/photo.jpg">`,
			`<img src="https://example.com/blog/images/photo.jpg">`,
		},
		{
			"relative href SQ",
			`<link href='../style.css'>`,
			`<link href='https://example.com/style.css'>`,
		},
		{
			"absolute unchanged",
			`<img src="https://cdn.example.com/img.png">`,
			`<img src="https://cdn.example.com/img.png">`,
		},
		{
			"protocol relative unchanged",
			`<script src="//cdn.example.com/lib.js">`,
			`<script src="//cdn.example.com/lib.js">`,
		},
		{
			"root relative unchanged",
			`<link href="/global.css">`,
			`<link href="/global.css">`,
		},
		{
			"data uri unchanged",
			`<img src="data:image/png;base64,abc">`,
			`<img src="data:image/png;base64,abc">`,
		},
		{
			"url() relative",
			`url(fonts/icon.woff2)`,
			`url("https://example.com/blog/fonts/icon.woff2")`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ResolveRelativeURLs(tt.input, baseURL)
			if result != tt.want {
				t.Errorf("got %q, want %q", result, tt.want)
			}
		})
	}
}

// TestRewriteHTMLFast_UnmappedPaths_Precompiled 验证兜底路径重写
func TestRewriteHTMLFast_UnmappedPaths_Precompiled(t *testing.T) {
	r := NewURLRewriter()
	r.SetPageID(100)
	r.SetTimestamp("20260401120000")
	r.SetBaseURL("https://example.com/page")

	// 不添加任何映射，测试兜底逻辑
	html := ` src="/assets/style.css" href="/js/app.js"`
	result := r.RewriteHTML(html)

	if result == html {
		t.Error("unmapped absolute paths should be rewritten")
	}
	if result != ` src="/archive/100/20260401120000mp_/https://example.com/assets/style.css" href="/archive/100/20260401120000mp_/https://example.com/js/app.js"` {
		t.Errorf("unexpected result: %s", result)
	}
}

// TestRewriteHTMLFast_SrcsetMultiValue_Precompiled 验证 srcset 多值重写
func TestRewriteHTMLFast_SrcsetMultiValue_Precompiled(t *testing.T) {
	r := NewURLRewriter()
	r.SetPageID(1)
	r.SetTimestamp("20260101000000")
	r.SetBaseURL("https://example.com")
	r.AddMapping("https://example.com/img-300.jpg", "resources/ab/cd/hash1.img")
	r.AddMapping("https://example.com/img-600.jpg", "resources/ab/cd/hash2.img")

	html := `<img srcset="https://example.com/img-300.jpg 300w, https://example.com/img-600.jpg 600w">`
	result := r.RewriteHTML(html)

	if result == html {
		t.Error("srcset values should be rewritten")
	}
	if !contains(result, "/archive/1/20260101000000mp_/https://example.com/img-300.jpg") {
		t.Errorf("300w image not rewritten: %s", result)
	}
	if !contains(result, "/archive/1/20260101000000mp_/https://example.com/img-600.jpg") {
		t.Errorf("600w image not rewritten: %s", result)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
