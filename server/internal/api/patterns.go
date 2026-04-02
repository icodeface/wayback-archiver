package api

import "regexp"

// Pre-compiled regex patterns for performance optimization.
// These patterns are compiled once at package initialization and reused throughout the application.
var (
	// HTML tag patterns
	bodyTagRe   = regexp.MustCompile(`(?i)(<body[^>]*>)`)
	headTagRe   = regexp.MustCompile(`(?i)(<head[^>]*>)`)
	htmlTagRe   = regexp.MustCompile(`(?i)(<html[^>]*>)`)
	scriptTagRe   = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	noscriptTagRe = regexp.MustCompile(`(?is)<noscript[^>]*>.*?</noscript>`)

	// Resource attribute patterns
	srcAttrRe    = regexp.MustCompile(`(?i)\ssrc=["']([^"']+)["']`)
	hrefAttrRe   = regexp.MustCompile(`(?i)\shref=["']([^"']+)["']`)
	srcsetAttrRe = regexp.MustCompile(`(?i)\ssrcset=["']([^"']+)["']`)
	posterAttrRe = regexp.MustCompile(`(?i)\sposter=["']([^"']+)["']`)
	dataAttrRe   = regexp.MustCompile(`(?i)\sdata=["']([^"']+)["']`)

	// CSS patterns
	cssURLRe      = regexp.MustCompile(`url\(['"]?([^'")\s]+)['"]?\)`)
	cssImportRe   = regexp.MustCompile(`@import\s+['"]([^'"]+)['"]`)
	cssFontFaceRe = regexp.MustCompile(`@font-face\s*\{[^}]*\}`)

	// Path patterns
	archivePathRe = regexp.MustCompile(`/archive/resources/`)

	// Meta patterns
	metaRefreshRe = regexp.MustCompile(`(?i)<meta[^>]*http-equiv=["']?refresh["']?[^>]*>`)
	metaCSPRe     = regexp.MustCompile(`(?i)<meta[^>]*http-equiv\s*=\s*["']?Content-Security-Policy["']?[^>]*>`)

	// ViewPage patterns (previously compiled per-request)
	baseTagRe        = regexp.MustCompile(`(?i)<base\s[^>]*>`)
	eventHandlerDQRe = regexp.MustCompile(`(?i)\s+on\w+\s*=\s*"[^"]*"`)
	eventHandlerSQRe = regexp.MustCompile(`(?i)\s+on\w+\s*=\s*'[^']*'`)
	jsProtocolRe     = regexp.MustCompile(`(?i)href\s*=\s*["']javascript:[^"']*["']`)
	lazyLoadRe       = regexp.MustCompile(`(?i)\s+loading\s*=\s*["']lazy["']`)
	sourceTagRe      = regexp.MustCompile(`(?is)<source[^>]*>`)
	videoBlockRe     = regexp.MustCompile(`(?is)<video\b([^>]*)>(.*?)</video>|<video\b([^>]*)/>`)
)
