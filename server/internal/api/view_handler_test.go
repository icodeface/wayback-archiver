package api

import (
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
