package api

import (
	"strings"
	"testing"
)

// TestPrecompiledRegex_BaseTag 验证预编译的 baseTagRe 正确移除 <base> 标签
func TestPrecompiledRegex_BaseTag(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"standard", `<base href="https://example.com/">`, ""},
		{"with target", `<base href="https://example.com/" target="_blank">`, ""},
		{"uppercase", `<BASE HREF="https://example.com/">`, ""},
		{"mixed", `<Base href="/">`, ""},
		{"no base", `<div>content</div>`, `<div>content</div>`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := baseTagRe.ReplaceAllString(tt.input, "")
			if result != tt.want {
				t.Errorf("got %q, want %q", result, tt.want)
			}
		})
	}
}

// TestPrecompiledRegex_EventHandlers 验证预编译的事件处理器正则
func TestPrecompiledRegex_EventHandlers(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			"onclick DQ",
			`<div onclick="alert(1)" class="box">text</div>`,
			`<div class="box">text</div>`,
		},
		{
			"onmouseover SQ",
			`<a onmouseover='highlight()' href="/page">link</a>`,
			`<a href="/page">link</a>`,
		},
		{
			"nested quotes",
			`<div onclick="window.open('https://example.com', '_blank')" class="desc">text</div>`,
			`<div class="desc">text</div>`,
		},
		{
			"case insensitive",
			`<div onClick="f()" ONMOUSEOVER="g()">text</div>`,
			`<div>text</div>`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := eventHandlerDQRe.ReplaceAllString(tt.input, "")
			result = eventHandlerSQRe.ReplaceAllString(result, "")
			if result != tt.want {
				t.Errorf("got %q, want %q", result, tt.want)
			}
		})
	}
}

// TestPrecompiledRegex_JsProtocol 验证 javascript: 链接移除
func TestPrecompiledRegex_JsProtocol(t *testing.T) {
	input := `<a href="javascript:void(0)">click</a>`
	result := jsProtocolRe.ReplaceAllString(input, `href="#"`)
	if !strings.Contains(result, `href="#"`) {
		t.Errorf("javascript: protocol should be replaced, got: %s", result)
	}
}

// TestPrecompiledRegex_LazyLoad 验证 lazy loading 属性移除
func TestPrecompiledRegex_LazyLoad(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{`<img loading="lazy" src="/img.png">`, `<img src="/img.png">`},
		{`<img loading='lazy' src="/img.png">`, `<img src="/img.png">`},
		{`<img LOADING="LAZY" src="/img.png">`, `<img src="/img.png">`},
		{`<img loading="eager" src="/img.png">`, `<img loading="eager" src="/img.png">`},
	}
	for _, tt := range tests {
		result := lazyLoadRe.ReplaceAllString(tt.input, "")
		if result != tt.want {
			t.Errorf("input %q: got %q, want %q", tt.input, result, tt.want)
		}
	}
}

// TestPrecompiledRegex_SourceTag 验证 <source> 标签移除
func TestPrecompiledRegex_SourceTag(t *testing.T) {
	input := `<picture><source srcset="/img.avif" type="image/avif"><source srcset="/img.webp" type="image/webp"><img src="/img.jpg"></picture>`
	result := sourceTagRe.ReplaceAllString(input, "")
	if strings.Contains(result, "<source") {
		t.Errorf("source tags should be removed, got: %s", result)
	}
	if !strings.Contains(result, `<img src="/img.jpg">`) {
		t.Errorf("img tag should be preserved, got: %s", result)
	}
}

// TestPrecompiledRegex_VideoBlock 验证 video 元素匹配
func TestPrecompiledRegex_VideoBlock(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		hasMatch bool
	}{
		{"with content", `<video autoplay><source src="v.mp4"></video>`, true},
		{"self-closing", `<video src="v.mp4"/>`, true},
		{"empty", `<video></video>`, true},
		{"no video", `<div>text</div>`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matched := videoBlockRe.MatchString(tt.input)
			if matched != tt.hasMatch {
				t.Errorf("match = %v, want %v", matched, tt.hasMatch)
			}
		})
	}
}

// TestPrecompiledRegex_LoadingOverlays 验证 SPA loading 覆盖层移除
func TestPrecompiledRegex_LoadingOverlays(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			"placeholder",
			`<div id="placeholder" style="background:black"><div>loading...</div></div><main>content</main>`,
			`<main>content</main>`,
		},
		{
			"ScriptLoadFailure",
			`<div id="ScriptLoadFailure"><p>Error</p></div><div id="app">app</div>`,
			`<div id="app">app</div>`,
		},
		{
			"no overlay",
			`<div id="app">content</div>`,
			`<div id="app">content</div>`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := removeLoadingOverlays(tt.input)
			if result != tt.want {
				t.Errorf("got %q, want %q", result, tt.want)
			}
		})
	}
}

// TestPrecompiledRegex_ExternalResources 验证外部资源移除
func TestPrecompiledRegex_ExternalResources(t *testing.T) {
	input := `<head>
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="dns-prefetch" href="https://cdn.example.com">
<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Inter">
<link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/bootstrap.css">
<link rel="stylesheet" href="/local/style.css">
</head>`

	result := removeExternalResources(input)

	if strings.Contains(result, "preconnect") {
		t.Error("preconnect should be removed")
	}
	if strings.Contains(result, "dns-prefetch") {
		t.Error("dns-prefetch should be removed")
	}
	if strings.Contains(result, "googleapis.com") {
		t.Error("external font link should be removed")
	}
	if strings.Contains(result, "jsdelivr.net") {
		t.Error("external CDN CSS should be removed")
	}
	if !strings.Contains(result, "/local/style.css") {
		t.Error("local stylesheet should be preserved")
	}
}
