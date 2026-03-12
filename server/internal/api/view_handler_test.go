package api

import (
	"regexp"
	"strings"
	"testing"
)

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

func TestFixOKXLayout(t *testing.T) {
	html := `<html><head><link rel="stylesheet" href="/archive/590/test.css"></head><body>
<div class="balance_okui balance_okui-tabs-var balance_okui-tabs ">
<div class="Balance_balanceBottom__vAGcz">content</div>
</div></body></html>`

	result := fixOKXLayout(html)

	if !strings.Contains(result, ".balance_okui-tabs { height: auto !important; }") {
		t.Error("Should inject CSS to fix .balance_okui-tabs height")
	}
	if !strings.Contains(result, `[class*="Balance_balanceBottom"] { min-height: auto !important; }`) {
		t.Error("Should inject CSS to fix Balance_balanceBottom min-height")
	}
	// 确保注入在 </head> 之前
	headIdx := strings.Index(result, "</head>")
	styleIdx := strings.Index(result, ".balance_okui-tabs { height: auto")
	if styleIdx > headIdx {
		t.Error("CSS should be injected before </head>")
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
