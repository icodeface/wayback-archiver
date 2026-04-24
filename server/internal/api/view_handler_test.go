package api

import (
	"regexp"
	"strings"
	"testing"

	"wayback/internal/storage"
)

func TestRewriteCSSForPage_RewritesMatchedSubresources(t *testing.T) {
	parser := storage.NewCSSParser()
	css := `.hero { background-image: url('../img/logo.png'); } .icon { background-image: url("https://cdn.example.com/icon.svg"); }`

	rewritten := rewriteCSSForPage(parser, css, "https://example.com/assets/css/app.css", func(resourceURL string) (string, bool) {
		switch resourceURL {
		case "https://example.com/assets/img/logo.png":
			return "resources/aa/bb/logo.img", true
		case "https://cdn.example.com/icon.svg":
			return "resources/cc/dd/icon.img", true
		default:
			return "", false
		}
	})

	if !strings.Contains(rewritten, `url('/archive/resources/aa/bb/logo.img')`) {
		t.Fatalf("relative CSS resource was not rewritten: %s", rewritten)
	}
	if !strings.Contains(rewritten, `url("/archive/resources/cc/dd/icon.img")`) {
		t.Fatalf("absolute CSS resource was not rewritten: %s", rewritten)
	}
}

func TestRewriteCSSForPage_PreservesUnmatchedURLs(t *testing.T) {
	parser := storage.NewCSSParser()
	css := `.keep { background-image: url('/archive/logo.png'); }`

	rewritten := rewriteCSSForPage(parser, css, "https://example.com/assets/css/app.css", func(resourceURL string) (string, bool) {
		return "", false
	})

	if rewritten != css {
		t.Fatalf("unmatched CSS should stay unchanged, got: %s", rewritten)
	}
}

func TestRewriteCSSForPage_RewritesImportWithoutRecursion(t *testing.T) {
	parser := storage.NewCSSParser()
	css := `@import url("./nested.css"); .hero { background-image: url('../img/logo.png'); }`
	var resolved []string

	rewritten := rewriteCSSForPage(parser, css, "https://example.com/assets/css/app.css", func(resourceURL string) (string, bool) {
		resolved = append(resolved, resourceURL)
		switch resourceURL {
		case "https://example.com/assets/css/nested.css":
			return "resources/aa/bb/nested.css", true
		case "https://example.com/assets/img/logo.png":
			return "resources/cc/dd/logo.img", true
		default:
			return "", false
		}
	})

	if !strings.Contains(rewritten, `@import url("/archive/resources/aa/bb/nested.css")`) {
		t.Fatalf("import URL was not rewritten: %s", rewritten)
	}
	if !strings.Contains(rewritten, `url('/archive/resources/cc/dd/logo.img')`) {
		t.Fatalf("image URL was not rewritten: %s", rewritten)
	}
	if len(resolved) != 2 {
		t.Fatalf("expected exactly 2 resolve attempts, got %d: %#v", len(resolved), resolved)
	}
}

func TestRewriteCSSForPage_SelfImportIsSinglePass(t *testing.T) {
	parser := storage.NewCSSParser()
	css := `@import url("./self.css");`
	resolveCalls := 0

	rewritten := rewriteCSSForPage(parser, css, "https://example.com/assets/css/self.css", func(resourceURL string) (string, bool) {
		resolveCalls++
		if resourceURL == "https://example.com/assets/css/self.css" {
			return "resources/aa/bb/self.css", true
		}
		return "", false
	})

	if !strings.Contains(rewritten, `@import url("/archive/resources/aa/bb/self.css")`) {
		t.Fatalf("self import was not rewritten: %s", rewritten)
	}
	if resolveCalls != 1 {
		t.Fatalf("expected single-pass rewrite, got %d resolve calls", resolveCalls)
	}
}

func TestRewriteCSSForPage_MutualImportsRemainSinglePass(t *testing.T) {
	parser := storage.NewCSSParser()
	resolveCalls := 0
	resolver := func(resourceURL string) (string, bool) {
		resolveCalls++
		switch resourceURL {
		case "https://example.com/assets/css/a.css":
			return "resources/aa/bb/a.css", true
		case "https://example.com/assets/css/b.css":
			return "resources/cc/dd/b.css", true
		default:
			return "", false
		}
	}

	aCSS := `@import url("./b.css"); .a { color: red; }`
	bCSS := `@import url("./a.css"); .b { color: blue; }`

	rewrittenA := rewriteCSSForPage(parser, aCSS, "https://example.com/assets/css/a.css", resolver)
	rewrittenB := rewriteCSSForPage(parser, bCSS, "https://example.com/assets/css/b.css", resolver)

	if !strings.Contains(rewrittenA, `@import url("/archive/resources/cc/dd/b.css")`) {
		t.Fatalf("A.css import was not rewritten: %s", rewrittenA)
	}
	if !strings.Contains(rewrittenB, `@import url("/archive/resources/aa/bb/a.css")`) {
		t.Fatalf("B.css import was not rewritten: %s", rewrittenB)
	}
	if resolveCalls != 2 {
		t.Fatalf("expected 2 resolve calls total, got %d", resolveCalls)
	}
	if strings.Count(rewrittenA, "/archive/resources/") != 1 || strings.Count(rewrittenB, "/archive/resources/") != 1 {
		t.Fatalf("mutual imports should be rewritten once per response: A=%q B=%q", rewrittenA, rewrittenB)
	}
}

func TestRewriteCSSForPage_RewritesQueryAndFragmentURLs(t *testing.T) {
	parser := storage.NewCSSParser()
	css := `@import url("./theme.css?v=123#dark"); .hero{background:url("../img/logo.png?size=2x#main")}`
	var resolved []string

	rewritten := rewriteCSSForPage(parser, css, "https://example.com/assets/css/app.css?v=1", func(resourceURL string) (string, bool) {
		resolved = append(resolved, resourceURL)
		switch resourceURL {
		case "https://example.com/assets/css/theme.css?v=123#dark":
			return "resources/11/22/theme.css", true
		case "https://example.com/assets/img/logo.png?size=2x#main":
			return "resources/33/44/logo.img", true
		default:
			return "", false
		}
	})

	if !strings.Contains(rewritten, `@import url("/archive/resources/11/22/theme.css")`) {
		t.Fatalf("query/hash import was not rewritten: %s", rewritten)
	}
	if !strings.Contains(rewritten, `url("/archive/resources/33/44/logo.img")`) {
		t.Fatalf("query/hash asset URL was not rewritten: %s", rewritten)
	}
	if len(resolved) != 2 {
		t.Fatalf("expected 2 resolved URLs, got %d: %#v", len(resolved), resolved)
	}
}

func TestRewriteCSSForPage_RewritesRootAndParentRelativePaths(t *testing.T) {
	parser := storage.NewCSSParser()
	css := `.root{background:url('/shared/root.png')} .parent{background:url('../../img/cover.jpg')}`

	rewritten := rewriteCSSForPage(parser, css, "https://example.com/static/css/nested/app.css", func(resourceURL string) (string, bool) {
		switch resourceURL {
		case "https://example.com/shared/root.png":
			return "resources/55/66/root.img", true
		case "https://example.com/static/img/cover.jpg":
			return "resources/77/88/cover.img", true
		default:
			return "", false
		}
	})

	if !strings.Contains(rewritten, `url('/archive/resources/55/66/root.img')`) {
		t.Fatalf("root-relative URL was not rewritten: %s", rewritten)
	}
	if !strings.Contains(rewritten, `url('/archive/resources/77/88/cover.img')`) {
		t.Fatalf("parent-relative URL was not rewritten: %s", rewritten)
	}
}

func TestResolveRelativeURL_HandlesAbsoluteAndInvalidInputs(t *testing.T) {
	tests := []struct {
		name     string
		baseURL  string
		relative string
		want     string
	}{
		{
			name:     "absolute URL stays unchanged",
			baseURL:  "https://example.com/assets/css/app.css",
			relative: "https://cdn.example.com/a.css",
			want:     "https://cdn.example.com/a.css",
		},
		{
			name:     "protocol relative resolves against base scheme",
			baseURL:  "https://example.com/assets/css/app.css",
			relative: "//cdn.example.com/a.css",
			want:     "https://cdn.example.com/a.css",
		},
		{
			name:     "invalid base returns empty",
			baseURL:  "://bad",
			relative: "./a.css",
			want:     "",
		},
		{
			name:     "invalid relative returns empty",
			baseURL:  "https://example.com/assets/css/app.css",
			relative: "http://[::1",
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveRelativeURL(tt.baseURL, tt.relative)
			if got != tt.want {
				t.Fatalf("resolveRelativeURL(%q, %q) = %q, want %q", tt.baseURL, tt.relative, got, tt.want)
			}
		})
	}
}

func TestFixNestedButtons_NoNesting(t *testing.T) {
	html := `<button class="a">click</button><button class="b">ok</button>`
	result := fixNestedButtons(html)
	if result != html {
		t.Errorf("Should not modify non-nested buttons, got: %s", result)
	}
}

func TestFixNestedButtons_SimpleNesting(t *testing.T) {
	html := `<button class="outer"><div><button class="inner">click</button></div></button>`
	result := fixNestedButtons(html)

	if strings.Contains(result, "<button class=\"inner\">") {
		t.Errorf("Inner button should be replaced with span, got: %s", result)
	}
	if !strings.Contains(result, "<span class=\"inner\">click</span>") {
		t.Errorf("Inner button should become span, got: %s", result)
	}
	if !strings.Contains(result, "<button class=\"outer\">") {
		t.Errorf("Outer button should be preserved, got: %s", result)
	}
}

func TestFixNestedButtons_MultipleNested(t *testing.T) {
	html := `<button><button>a</button><button>b</button></button>`
	result := fixNestedButtons(html)

	if strings.Count(result, "<button") != 1 {
		t.Errorf("Should have exactly 1 button, got: %s", result)
	}
	if strings.Count(result, "<span") != 2 {
		t.Errorf("Should have 2 spans, got: %s", result)
	}
}

func TestFixNestedButtons_ThreeLevels(t *testing.T) {
	html := `<button><button><button>deep</button></button></button>`
	result := fixNestedButtons(html)

	if strings.Count(result, "<button") != 1 {
		t.Errorf("Should have exactly 1 button, got: %s", result)
	}
	if strings.Count(result, "</button>") != 1 {
		t.Errorf("Should have exactly 1 </button>, got: %s", result)
	}
	if strings.Count(result, "<span") != 2 {
		t.Errorf("Should have 2 spans for nested buttons, got: %s", result)
	}
}

func TestFixNestedButtons_CaseInsensitive(t *testing.T) {
	html := `<BUTTON><Button>inner</Button></BUTTON>`
	result := fixNestedButtons(html)

	if strings.Count(strings.ToLower(result), "<button") != 1 {
		t.Errorf("Should handle case-insensitive buttons, got: %s", result)
	}
}

// --- 内联事件处理器移除测试 ---

func TestRemoveEventHandlers_Simple(t *testing.T) {
	// 简单的双引号 onclick
	html := `<div onclick="alert(1)" class="box">content</div>`
	result := removeEventHandlers(html)
	expected := `<div class="box">content</div>`
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

func TestRemoveEventHandlers_NestedQuotes(t *testing.T) {
	// 双引号属性值内包含单引号 — 这是导致 page 899 布局崩溃的根因
	html := `<div onclick="window.open('https://example.com', '_blank')" class="desc">text</div>`
	result := removeEventHandlers(html)
	expected := `<div class="desc">text</div>`
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

func TestRemoveEventHandlers_SingleQuoteAttr(t *testing.T) {
	// 单引号包裹的事件属性
	html := `<a onmouseover='this.style.color="red"' href="/page">link</a>`
	result := removeEventHandlers(html)
	expected := `<a href="/page">link</a>`
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

func TestRemoveEventHandlers_Multiple(t *testing.T) {
	// 同一元素上多个事件处理器
	html := `<input onfocus="f()" onblur="g()" type="text">`
	result := removeEventHandlers(html)
	expected := `<input type="text">`
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

func TestRemoveEventHandlers_PreservesNonEvent(t *testing.T) {
	// 不应误删非事件属性（如 "one-click" class）
	html := `<div class="one-click" id="main">content</div>`
	result := removeEventHandlers(html)
	if result != html {
		t.Errorf("Should not modify non-event attributes, got %q", result)
	}
}

func TestRemoveEventHandlers_V2EXAdCase(t *testing.T) {
	// 还原 page 899 的实际场景：onclick 内嵌 window.open + 单引号 URL
	html := `<div class="pro-unit flex-one-row">
<div class="pro-unit-small-image"><a href="https://zhale.me"><img src="/img.png"></a></div>
<div onclick="window.open('https://zhale.me/invite/?code=4ZV2265S2222', '_blank')" class="pro-unit-description">监控平台</div>
</div>`
	result := removeEventHandlers(html)

	// div 标签必须完整保留，不能出现 <divhttps://... 这种畸形标签
	if strings.Contains(result, "<divhttps") {
		t.Fatal("Bug reproduced: <div> tag was mangled into <divhttps...>")
	}
	if !strings.Contains(result, `<div class="pro-unit-description">监控平台</div>`) {
		t.Errorf("onclick should be removed but div preserved, got %q", result)
	}
}

func TestRemoveEventHandlers_CaseInsensitive(t *testing.T) {
	html := `<div onClick="f()" ONMOUSEOVER="g()">text</div>`
	result := removeEventHandlers(html)
	expected := `<div>text</div>`
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

// removeEventHandlers 提取事件处理器移除逻辑，方便测试
func removeEventHandlers(html string) string {
	eventHandlerDQ := regexp.MustCompile(`(?i)\s+on\w+\s*=\s*"[^"]*"`)
	eventHandlerSQ := regexp.MustCompile(`(?i)\s+on\w+\s*=\s*'[^']*'`)
	html = eventHandlerDQ.ReplaceAllString(html, "")
	html = eventHandlerSQ.ReplaceAllString(html, "")
	return html
}

func TestFixNestedButtons_RealWorldPopover(t *testing.T) {
	// Simulates the actual Vben Admin popover trigger pattern
	html := `<header><button class="" id="reka-popover-trigger-v-3" type="button"><div class="flex-center"><button class="bell-button text-foreground"><svg>icon</svg></button></div></button></header>`
	result := fixNestedButtons(html)

	if strings.Contains(result, `<button class="bell-button`) {
		t.Errorf("Nested bell-button should be replaced with span, got: %s", result)
	}
	if !strings.Contains(result, `<span class="bell-button`) {
		t.Errorf("Nested bell-button should become span, got: %s", result)
	}
	// Outer button should remain
	if !strings.Contains(result, `<button class="" id="reka-popover-trigger-v-3"`) {
		t.Errorf("Outer popover button should be preserved, got: %s", result)
	}
}

// --- 滚动动画 opacity 修复测试 ---

func TestFixScrollAnimationOpacity_Basic(t *testing.T) {
	html := `<div data-animate-item="" style="opacity: 0; transform: translate(0px, 50px);">content</div>`
	result := fixScrollAnimationOpacity(html)
	if strings.Contains(result, "opacity: 0") {
		t.Errorf("opacity: 0 should be removed, got: %s", result)
	}
	if strings.Contains(result, "transform:") {
		t.Errorf("transform should be removed, got: %s", result)
	}
	if !strings.Contains(result, "content") {
		t.Errorf("content should be preserved, got: %s", result)
	}
}

func TestFixScrollAnimationOpacity_AnimateChild(t *testing.T) {
	html := `<h3 data-animate-child="" style="translate: none; rotate: none; scale: none; transform: translate(0px, 20px); opacity: 0;">Title</h3>`
	result := fixScrollAnimationOpacity(html)
	if strings.Contains(result, "opacity: 0") {
		t.Errorf("opacity: 0 should be removed, got: %s", result)
	}
	if strings.Contains(result, "translate:") || strings.Contains(result, "rotate:") || strings.Contains(result, "scale:") {
		t.Errorf("animation properties should be removed, got: %s", result)
	}
}

func TestFixScrollAnimationOpacity_NoAnimateAttr(t *testing.T) {
	// 没有 data-animate-* 属性的元素不应被修改
	html := `<div style="opacity: 0; transform: translate(0px, 50px);">hidden</div>`
	result := fixScrollAnimationOpacity(html)
	if result != html {
		t.Errorf("Should not modify elements without data-animate-*, got: %s", result)
	}
}

func TestFixScrollAnimationOpacity_VisibleElement(t *testing.T) {
	// opacity: 1 的动画元素不应被修改
	html := `<div data-animate-item="" style="opacity: 1;">visible</div>`
	result := fixScrollAnimationOpacity(html)
	if result != html {
		t.Errorf("Should not modify visible elements, got: %s", result)
	}
}

func TestFixScrollAnimationOpacity_PreservesOtherStyles(t *testing.T) {
	html := `<div data-animate-item="" style="padding-top:20px;opacity: 0;display:flex;transform:matrix(1, 0, 0, 1, 0, 50)">text</div>`
	result := fixScrollAnimationOpacity(html)
	if strings.Contains(result, "opacity") {
		t.Errorf("opacity should be removed, got: %s", result)
	}
	if !strings.Contains(result, "padding-top:20px") {
		t.Errorf("padding-top should be preserved, got: %s", result)
	}
	if !strings.Contains(result, "display:flex") {
		t.Errorf("display:flex should be preserved, got: %s", result)
	}
}

func TestFixScrollAnimationOpacity_DataAnimateNoSuffix(t *testing.T) {
	html := `<div data-animate="" class="w-full flex" style="translate: none; rotate: none; scale: none; transform: translate(0px, 50px); opacity: 0;;margin-top:24px">text</div>`
	result := fixScrollAnimationOpacity(html)
	if strings.Contains(result, "opacity: 0") {
		t.Errorf("opacity: 0 should be removed, got: %s", result)
	}
	if strings.Contains(result, "transform:") {
		t.Errorf("transform should be removed, got: %s", result)
	}
	if !strings.Contains(result, "margin-top:24px") {
		t.Errorf("margin-top should be preserved, got: %s", result)
	}
}

func TestFixScrollAnimationOpacity_DataAnimateGroup(t *testing.T) {
	html := `<div data-animate-group="" style="opacity: 0; transform: translate(0px, 30px);">group</div>`
	result := fixScrollAnimationOpacity(html)
	if strings.Contains(result, "opacity: 0") {
		t.Errorf("opacity: 0 should be removed, got: %s", result)
	}
}

func TestFixScrollAnimationOpacity_SVGChartLine(t *testing.T) {
	html := `<path class="chart-line" d="M49.0 397.3" stroke="#74C375" stroke-width="1.5" style="opacity: 0; stroke-dashoffset: 1100px; stroke-dasharray: 1099.62;;box-sizing:border-box"></path>`
	result := fixScrollAnimationOpacity(html)
	if strings.Contains(result, "opacity: 0") {
		t.Errorf("opacity: 0 should be removed from SVG chart line, got: %s", result)
	}
	if strings.Contains(result, "stroke-dashoffset") {
		t.Errorf("stroke-dashoffset should be removed, got: %s", result)
	}
	if !strings.Contains(result, "stroke-dasharray") {
		t.Errorf("stroke-dasharray should be preserved, got: %s", result)
	}
}

func TestFixScrollAnimationOpacity_SVGChartLegend(t *testing.T) {
	html := `<circle class="chart-legend" cx="205.5" cy="165" r="2.5" fill="#F1F1F1" style="opacity: 0; stroke-dashoffset: 0;;box-sizing:border-box"></circle>`
	result := fixScrollAnimationOpacity(html)
	if strings.Contains(result, "opacity: 0") {
		t.Errorf("opacity: 0 should be removed from SVG chart legend, got: %s", result)
	}
}

func TestFixScrollAnimationOpacity_SVGNoStrokeDash(t *testing.T) {
	// SVG element with opacity: 0 but no stroke-dashoffset — should also be fixed
	// SVG graphic elements rarely use inline opacity: 0 intentionally
	html := `<path d="M0 0" style="opacity: 0; fill: red;"></path>`
	result := fixScrollAnimationOpacity(html)
	if strings.Contains(result, "opacity: 0") {
		t.Errorf("opacity: 0 should be removed from SVG element, got: %s", result)
	}
	if !strings.Contains(result, "fill: red") {
		t.Errorf("fill should be preserved, got: %s", result)
	}
}

func TestFixScrollAnimationOpacity_SVGCustomElement(t *testing.T) {
	// 自定义元素名以 SVG 标签开头（如 path-component）不应被匹配
	html := `<path-component style="opacity: 0; display: none;">hidden</path-component>`
	result := fixScrollAnimationOpacity(html)
	if result != html {
		t.Errorf("Custom element should not be modified, got: %s", result)
	}
}

func TestFixScrollAnimationOpacity_SVGUpperCase(t *testing.T) {
	// 大写 SVG 标签也应被匹配
	html := `<PATH d="M0 0" style="opacity: 0; fill: red;"></PATH>`
	result := fixScrollAnimationOpacity(html)
	if strings.Contains(result, "opacity: 0") {
		t.Errorf("opacity: 0 should be removed from uppercase SVG element, got: %s", result)
	}
}

func TestFixScrollAnimationOpacity_LeadingWhitespace(t *testing.T) {
	html := `<div data-animate-item="" style="  opacity: 0; color: red;">text</div>`
	result := fixScrollAnimationOpacity(html)
	if strings.Contains(result, "opacity: 0") {
		t.Errorf("opacity: 0 with leading whitespace should be removed, got: %s", result)
	}
	if !strings.Contains(result, "color: red") {
		t.Errorf("color should be preserved, got: %s", result)
	}
}

func TestHideVideoElements_DisablesAutoplayAndAddsControls(t *testing.T) {
	html := `<video autoplay src="/video.mp4"></video>`
	result := hideVideoElements(html)

	if strings.Contains(strings.ToLower(result), "autoplay") {
		t.Fatalf("autoplay should be removed, got: %s", result)
	}
	if !strings.Contains(strings.ToLower(result), "controls") {
		t.Fatalf("controls should be added for manual playback, got: %s", result)
	}
	if !strings.Contains(result, `src="/video.mp4"`) {
		t.Fatalf("video src should be preserved, got: %s", result)
	}
}

func TestHideVideoElements_PreservesExistingControls(t *testing.T) {
	html := `<video controls autoplay><source src="/video.mp4"></video>`
	result := hideVideoElements(html)

	if strings.Contains(strings.ToLower(result), "autoplay") {
		t.Fatalf("autoplay should be removed, got: %s", result)
	}
	if strings.Count(strings.ToLower(result), "controls") != 1 {
		t.Fatalf("controls should not be duplicated, got: %s", result)
	}
}

func TestHideVideoElements_HidesEmptyVideo(t *testing.T) {
	html := `<video autoplay></video>`
	result := hideVideoElements(html)

	if !strings.Contains(result, `style="display:none!important"`) {
		t.Fatalf("empty video should be hidden, got: %s", result)
	}
	if strings.Contains(strings.ToLower(result), "autoplay") {
		t.Fatalf("autoplay should be removed from hidden empty video, got: %s", result)
	}
}

func TestHideVideoElements_AddsControlsWhenVideoUsesSourceChild(t *testing.T) {
	html := `<video autoplay><source src="/video.mp4" type="video/mp4"></video>`
	result := hideVideoElements(html)

	if strings.Contains(strings.ToLower(result), "autoplay") {
		t.Fatalf("autoplay should be removed, got: %s", result)
	}
	if !strings.Contains(strings.ToLower(result), "controls") {
		t.Fatalf("controls should be added for video with source child, got: %s", result)
	}
	if !strings.Contains(result, `<source src="/video.mp4" type="video/mp4">`) {
		t.Fatalf("source child should be preserved, got: %s", result)
	}
}

func TestHideVideoElements_SelfClosingVideo(t *testing.T) {
	html := `<video autoplay src="/video.mp4"/>`
	result := hideVideoElements(html)

	if strings.Contains(strings.ToLower(result), "autoplay") {
		t.Fatalf("autoplay should be removed from self-closing video, got: %s", result)
	}
	if !strings.Contains(strings.ToLower(result), "controls") {
		t.Fatalf("controls should be added to self-closing video, got: %s", result)
	}
	if !strings.Contains(result, `src="/video.mp4"`) {
		t.Fatalf("src should be preserved on self-closing video, got: %s", result)
	}
}

func TestHideVideoElements_CaseInsensitiveAttributes(t *testing.T) {
	html := `<video AUTOPLAY SRC="/video.mp4"></video>`
	result := hideVideoElements(html)

	if strings.Contains(strings.ToLower(result), "autoplay") {
		t.Fatalf("autoplay should be removed case-insensitively, got: %s", result)
	}
	if !strings.Contains(strings.ToLower(result), "controls") {
		t.Fatalf("controls should be added with uppercase attrs, got: %s", result)
	}
	if !strings.Contains(result, `SRC="/video.mp4"`) {
		t.Fatalf("original src attribute should be preserved, got: %s", result)
	}
}

func TestHideVideoElements_PreservesExistingControlsValue(t *testing.T) {
	html := `<video controls="controls" autoplay src="/video.mp4"></video>`
	result := hideVideoElements(html)

	if strings.Contains(strings.ToLower(result), "autoplay") {
		t.Fatalf("autoplay should be removed, got: %s", result)
	}
	if len(controlsAttrRe.FindAllString(result, -1)) != 1 {
		t.Fatalf("existing controls attribute should be preserved without duplication, got: %s", result)
	}
	if !strings.Contains(result, `controls="controls"`) {
		t.Fatalf("existing controls value should be preserved, got: %s", result)
	}
}

func TestFixUnrewrittenSrcset_OnlyRemovesPictureSources(t *testing.T) {
	html := `<picture><source srcset="/img.avif" type="image/avif"><source srcset="/img.webp" type="image/webp"><img src="/img.jpg"></picture><video><source src="/video.mp4" type="video/mp4"></video><audio><source src="/audio.mp3" type="audio/mpeg"></audio>`
	result := fixUnrewrittenSrcset(html)

	if strings.Contains(result, `<picture><source`) {
		t.Fatalf("picture source tags should be removed, got: %s", result)
	}
	if !strings.Contains(result, `<img src="/img.jpg">`) {
		t.Fatalf("picture fallback img should be preserved, got: %s", result)
	}
	if !strings.Contains(result, `<video><source src="/video.mp4" type="video/mp4"></video>`) {
		t.Fatalf("video source should be preserved, got: %s", result)
	}
	if !strings.Contains(result, `<audio><source src="/audio.mp3" type="audio/mpeg"></audio>`) {
		t.Fatalf("audio source should be preserved, got: %s", result)
	}
}

func TestSanitizeArchivedHTML_PreservesMediaSourcesOutsidePicture(t *testing.T) {
	html := `<picture><source srcset="/img.avif" type="image/avif"><img src="/img.jpg"></picture><video autoplay><source src="/video.mp4" type="video/mp4"></video><audio><source src="/audio.mp3" type="audio/mpeg"></audio>`
	result := sanitizeArchivedHTML(html)

	if strings.Contains(result, `<picture><source`) {
		t.Fatalf("picture source tags should be removed during sanitize, got: %s", result)
	}
	if !strings.Contains(result, `<img src="/img.jpg">`) {
		t.Fatalf("picture fallback img should remain after sanitize, got: %s", result)
	}
	if strings.Contains(strings.ToLower(result), "autoplay") {
		t.Fatalf("video autoplay should be removed during sanitize, got: %s", result)
	}
	if !strings.Contains(strings.ToLower(result), "<video controls>") {
		t.Fatalf("video should remain playable with controls after sanitize, got: %s", result)
	}
	if !strings.Contains(result, `<source src="/video.mp4" type="video/mp4">`) {
		t.Fatalf("video source should remain after sanitize, got: %s", result)
	}
	if !strings.Contains(result, `<audio><source src="/audio.mp3" type="audio/mpeg"></audio>`) {
		t.Fatalf("audio source should remain after sanitize, got: %s", result)
	}
}

func TestRemoveUnsupportedEmbeddedContent_RemovesObjectEmbedButKeepsIframe(t *testing.T) {
	html := `<div><iframe src="https://example.com/app"></iframe><object data="//cdn.example.com/plugin.swf"><param name="wmode" value="transparent"></object><embed src="https://example.com/movie.swf"><img src="/archive/1/20260414mp_/https://example.com/logo.png"></div>`
	result := removeUnsupportedEmbeddedContent(html)

	if !strings.Contains(strings.ToLower(result), "<iframe") {
		t.Fatalf("iframe should be preserved, got: %s", result)
	}
	if strings.Contains(strings.ToLower(result), "<object") {
		t.Fatalf("object should be removed, got: %s", result)
	}
	if strings.Contains(strings.ToLower(result), "<embed") {
		t.Fatalf("embed should be removed, got: %s", result)
	}
	if !strings.Contains(result, `<img src="/archive/1/20260414mp_/https://example.com/logo.png">`) {
		t.Fatalf("non-embedded archived content should be preserved, got: %s", result)
	}
}

func TestRemoveUnsupportedEmbeddedContent_RemovesFrameCaseInsensitively(t *testing.T) {
	html := `<FRAME SRC="https://example.com/legacy"></FRAME><p>ok</p>`
	result := removeUnsupportedEmbeddedContent(html)

	if strings.Contains(strings.ToLower(result), "<frame") {
		t.Fatalf("frame should be removed case-insensitively, got: %s", result)
	}
	if !strings.Contains(result, `<p>ok</p>`) {
		t.Fatalf("surrounding content should be preserved, got: %s", result)
	}
}

func TestRemoveUnarchivedExternalCSSReferences_RemovesExternalCSSURLs(t *testing.T) {
	html := `<style>.ok{background:url("/archive/1/20260414mp_/https://example.com/logo.png")}.bad{background:url(https://cdn.example.com/missing.png)}@import url("https://cdn.example.com/theme.css");</style><div style="background-image:url(//cdn.example.com/banner.jpg)"></div>`
	result := removeUnarchivedExternalCSSReferences(html)

	if !strings.Contains(result, `url("/archive/1/20260414mp_/https://example.com/logo.png")`) {
		t.Fatalf("archived CSS URL should be preserved, got: %s", result)
	}
	if strings.Contains(result, `https://cdn.example.com/missing.png`) {
		t.Fatalf("external CSS url() should be removed, got: %s", result)
	}
	if strings.Contains(result, `//cdn.example.com/banner.jpg`) {
		t.Fatalf("protocol-relative CSS url() should be removed, got: %s", result)
	}
	if strings.Contains(result, `@import url("https://cdn.example.com/theme.css")`) {
		t.Fatalf("external @import should be removed, got: %s", result)
	}
	if strings.Count(result, `url("")`) != 3 {
		t.Fatalf("expected three stripped url() placeholders, got: %s", result)
	}
}

// --- CSP meta 标签移除测试 ---

func TestRemoveCSPMeta(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "upgrade-insecure-requests",
			input: `<head><meta http-equiv="Content-Security-Policy" content="upgrade-insecure-requests"><title>Test</title></head>`,
			want:  `<head><title>Test</title></head>`,
		},
		{
			name:  "single quotes",
			input: `<head><meta http-equiv='Content-Security-Policy' content='upgrade-insecure-requests'><title>Test</title></head>`,
			want:  `<head><title>Test</title></head>`,
		},
		{
			name:  "no quotes on http-equiv",
			input: `<head><meta http-equiv=Content-Security-Policy content="upgrade-insecure-requests"><title>Test</title></head>`,
			want:  `<head><title>Test</title></head>`,
		},
		{
			name:  "case insensitive",
			input: `<head><META HTTP-EQUIV="Content-Security-Policy" CONTENT="upgrade-insecure-requests"></head>`,
			want:  `<head></head>`,
		},
		{
			name:  "complex CSP directive",
			input: `<head><meta http-equiv="Content-Security-Policy" content="default-src 'self'; upgrade-insecure-requests"></head>`,
			want:  `<head></head>`,
		},
		{
			name:  "preserves other meta tags",
			input: `<head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="upgrade-insecure-requests"><meta name="viewport" content="width=device-width"></head>`,
			want:  `<head><meta charset="utf-8"><meta name="viewport" content="width=device-width"></head>`,
		},
		{
			name:  "weibo real case with operaUserStyle",
			input: `<meta http-equiv="Content-Security-Policy" content="upgrade-insecure-requests"><style type="text/css" id="operaUserStyle"></style>`,
			want:  `<style type="text/css" id="operaUserStyle"></style>`,
		},
		{
			name:  "no CSP meta — unchanged",
			input: `<head><meta charset="utf-8"><meta http-equiv="X-UA-Compatible" content="IE=edge"></head>`,
			want:  `<head><meta charset="utf-8"><meta http-equiv="X-UA-Compatible" content="IE=edge"></head>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := metaCSPRe.ReplaceAllString(tt.input, "")
			if result != tt.want {
				t.Errorf("got  %q\nwant %q", result, tt.want)
			}
		})
	}
}
