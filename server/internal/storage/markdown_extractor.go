package storage

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/strikethrough"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/table"
	"golang.org/x/net/html"
)

var (
	multiNewlineRe = regexp.MustCompile(`\n{3,}`)
	mdConverter    = converter.NewConverter(
		converter.WithPlugins(
			base.NewBasePlugin(),
			commonmark.NewCommonmarkPlugin(),
			table.NewTablePlugin(),
			strikethrough.NewStrikethroughPlugin(),
		),
	)
)

// ExtractMarkdown 从 HTML 中提取正文内容并转换为 Markdown 格式
func ExtractMarkdown(htmlContent string) string {
	htmlContent = selectMarkdownHTML(htmlContent)

	markdown, err := mdConverter.ConvertString(htmlContent)
	if err != nil {
		return ""
	}

	// 合并连续空行
	markdown = multiNewlineRe.ReplaceAllString(markdown, "\n\n")
	return strings.TrimSpace(markdown) + "\n"
}

func selectMarkdownHTML(htmlContent string) string {
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return removeNonContentTags(htmlContent)
	}

	removeNonContentNodes(doc)
	demoteLayoutTables(doc)

	if selected := bestMarkdownNode(doc); selected != nil {
		return renderHTMLNode(selected)
	}
	if selected := firstNodeByTag(doc, "body"); selected != nil {
		return renderHTMLNode(selected)
	}

	return renderHTMLNode(doc)
}

func removeNonContentTags(htmlContent string) string {
	for tag := range nonContentTags {
		re := regexp.MustCompile(`(?is)<` + tag + `\b[^>]*>.*?</` + tag + `>`)
		htmlContent = re.ReplaceAllString(htmlContent, "")
		re2 := regexp.MustCompile(`(?i)<` + tag + `\b[^>]*/?>`)
		htmlContent = re2.ReplaceAllString(htmlContent, "")
	}
	return htmlContent
}

var nonContentTags = map[string]struct{}{
	"script":   {},
	"style":    {},
	"noscript": {},
	"svg":      {},
	"nav":      {},
	"aside":    {},
	"footer":   {},
	"iframe":   {},
	"object":   {},
	"embed":    {},
	"canvas":   {},
	"template": {},
	"form":     {},
	"button":   {},
	"input":    {},
	"select":   {},
	"textarea": {},
}

func removeNonContentNodes(n *html.Node) {
	for child := n.FirstChild; child != nil; {
		next := child.NextSibling
		if shouldRemoveMarkdownNode(child) {
			n.RemoveChild(child)
		} else {
			removeNonContentNodes(child)
		}
		child = next
	}
}

func shouldRemoveMarkdownNode(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return false
	}
	if _, ok := nonContentTags[strings.ToLower(n.Data)]; ok {
		return true
	}
	for _, attr := range n.Attr {
		key := strings.ToLower(attr.Key)
		value := strings.ToLower(strings.TrimSpace(attr.Val))
		if key == "hidden" {
			return true
		}
		if key == "aria-hidden" && value == "true" {
			return true
		}
		if key == "style" && isHiddenStyle(value) {
			return true
		}
		if key == "type" && value == "hidden" {
			return true
		}
		if key == "role" && isNonContentRole(value) {
			return true
		}
	}
	return false
}

func isHiddenStyle(style string) bool {
	normalized := strings.ReplaceAll(strings.ToLower(style), " ", "")
	return strings.Contains(normalized, "display:none") ||
		strings.Contains(normalized, "visibility:hidden")
}

func isNonContentRole(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "navigation", "banner", "complementary", "contentinfo", "search", "form", "dialog", "alert", "tooltip":
		return true
	default:
		return false
	}
}

func bestMarkdownNode(root *html.Node) *html.Node {
	var selected *html.Node
	var selectedScore float64
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if isMarkdownCandidate(n) {
			if score := markdownCandidateScore(n); score > selectedScore {
				selected = n
				selectedScore = score
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return selected
}

func isMarkdownCandidate(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return false
	}

	tag := strings.ToLower(n.Data)
	switch tag {
	case "article", "main", "section", "div", "td", "body":
		return true
	}

	return nodeRole(n) == "main" || containsAny(nodeClassID(n), positiveContentHints)
}

type markdownMetrics struct {
	textLen          int
	linkTextLen      int
	blockTextLen     int
	blockCount       int
	headingCount     int
	formControlCount int
}

func markdownCandidateScore(n *html.Node) float64 {
	metrics := collectMarkdownMetrics(n)
	if metrics.textLen < 80 && !isStrongMarkdownCandidate(n) && !strings.EqualFold(n.Data, "body") {
		return 0
	}

	score := float64(metrics.textLen) +
		float64(metrics.blockTextLen)*0.8 +
		float64(metrics.blockCount)*80 +
		float64(metrics.headingCount)*160

	switch strings.ToLower(n.Data) {
	case "article":
		score += 900
		score *= 1.8
	case "main":
		score += 700
	case "section":
		score += 180
	case "body":
		score *= 0.35
	}

	if nodeRole(n) == "main" {
		score += 650
	}
	if nodeID(n) == "main" {
		score += 2000
	}

	classID := nodeClassID(n)
	if containsAny(classID, positiveContentHints) {
		score += 450
	}
	if containsAny(classID, negativeContentHints) {
		score *= 0.35
	}

	if metrics.textLen > 0 && metrics.linkTextLen > 0 {
		linkDensity := float64(metrics.linkTextLen) / float64(metrics.textLen)
		if linkDensity > 0.2 {
			score *= 1 - minFloat(linkDensity, 0.85)
		}
	}

	if metrics.formControlCount > 0 {
		score -= float64(metrics.formControlCount) * 300
	}

	return score
}

func isStrongMarkdownCandidate(n *html.Node) bool {
	tag := strings.ToLower(n.Data)
	return tag == "article" || tag == "main" || nodeRole(n) == "main"
}

func collectMarkdownMetrics(n *html.Node) markdownMetrics {
	var metrics markdownMetrics
	var walk func(*html.Node, bool, bool)
	walk = func(node *html.Node, inLink, inBlock bool) {
		if node.Type == html.ElementNode {
			tag := strings.ToLower(node.Data)
			if tag == "a" {
				inLink = true
			}
			if isTextBlockTag(tag) {
				inBlock = true
				metrics.blockCount++
			}
			if isHeadingTag(tag) {
				metrics.headingCount++
			}
			if isFormControlTag(tag) {
				metrics.formControlCount++
			}
		}

		if node.Type == html.TextNode {
			textLen := len(strings.TrimSpace(node.Data))
			metrics.textLen += textLen
			if inLink {
				metrics.linkTextLen += textLen
			}
			if inBlock {
				metrics.blockTextLen += textLen
			}
		}

		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child, inLink, inBlock)
		}
	}
	walk(n, false, false)
	return metrics
}

func isTextBlockTag(tag string) bool {
	switch tag {
	case "p", "li", "pre", "blockquote":
		return true
	default:
		return false
	}
}

func isHeadingTag(tag string) bool {
	switch tag {
	case "h1", "h2", "h3", "h4", "h5", "h6":
		return true
	default:
		return false
	}
}

func isFormControlTag(tag string) bool {
	switch tag {
	case "form", "button", "input", "select", "textarea":
		return true
	default:
		return false
	}
}

func demoteLayoutTables(n *html.Node) {
	if n.Type == html.ElementNode && strings.EqualFold(n.Data, "table") && !isSemanticTable(n) {
		n.Data = "div"
		n.Attr = filterTableLayoutAttrs(n.Attr)
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		demoteLayoutTables(child)
	}
}

func isSemanticTable(n *html.Node) bool {
	role := nodeRole(n)
	if role == "table" || role == "grid" {
		return true
	}
	if containsAny(nodeClassID(n), []string{"data-table", "markdown-table", "table-data"}) {
		return true
	}

	var found bool
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if found || node.Type != html.ElementNode {
			return
		}
		switch strings.ToLower(node.Data) {
		case "th", "thead", "caption":
			found = true
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return found
}

func filterTableLayoutAttrs(attrs []html.Attribute) []html.Attribute {
	filtered := attrs[:0]
	for _, attr := range attrs {
		switch strings.ToLower(attr.Key) {
		case "cellpadding", "cellspacing", "border", "width", "height", "align", "valign":
			continue
		default:
			filtered = append(filtered, attr)
		}
	}
	return filtered
}

func firstNodeByTag(root *html.Node, tag string) *html.Node {
	var found *html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if found != nil {
			return
		}
		if n.Type == html.ElementNode && strings.EqualFold(n.Data, tag) {
			found = n
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return found
}

func markdownTextLen(n *html.Node) int {
	var length int
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.TextNode {
			length += len(strings.TrimSpace(node.Data))
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return length
}

func nodeRole(n *html.Node) string {
	for _, attr := range n.Attr {
		if strings.EqualFold(attr.Key, "role") {
			return strings.ToLower(strings.TrimSpace(attr.Val))
		}
	}
	return ""
}

func nodeID(n *html.Node) string {
	for _, attr := range n.Attr {
		if strings.EqualFold(attr.Key, "id") {
			return strings.ToLower(strings.TrimSpace(attr.Val))
		}
	}
	return ""
}

func nodeClassID(n *html.Node) string {
	var parts []string
	for _, attr := range n.Attr {
		if strings.EqualFold(attr.Key, "class") || strings.EqualFold(attr.Key, "id") {
			parts = append(parts, strings.ToLower(attr.Val))
		}
	}
	return strings.Join(parts, " ")
}

func containsAny(value string, hints []string) bool {
	if value == "" {
		return false
	}
	for _, hint := range hints {
		if strings.Contains(value, hint) {
			return true
		}
	}
	return false
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func renderHTMLNode(n *html.Node) string {
	var buf bytes.Buffer
	if err := html.Render(&buf, n); err != nil {
		return ""
	}
	return buf.String()
}

var positiveContentHints = []string{
	"article",
	"content",
	"main",
	"post",
	"entry",
	"story",
	"body",
	"markdown",
	"readme",
	"document",
	"docs",
	"discussion",
	"topic",
}

var negativeContentHints = []string{
	"nav",
	"navbar",
	"menu",
	"sidebar",
	"aside",
	"footer",
	"header",
	"toolbar",
	"breadcrumb",
	"pagination",
	"pager",
	"advert",
	"ads",
	"promo",
	"sponsor",
	"campaign",
	"social",
	"share",
	"modal",
	"popup",
	"cookie",
	"login",
	"signin",
	"signup",
	"subscribe",
	"comment-form",
	"reply-box",
	"search",
	"topbar",
	"rightbar",
	"leftbar",
	"bottom",
	"related",
	"recommend",
}
