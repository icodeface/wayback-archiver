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

	if selected := largestNodeByTag(doc, "article"); selected != nil {
		return renderHTMLNode(selected)
	}
	if selected := largestNodeByTag(doc, "main"); selected != nil {
		return renderHTMLNode(selected)
	}
	if selected := largestNodeByRole(doc, "main"); selected != nil {
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
	"footer":   {},
	"iframe":   {},
	"object":   {},
	"embed":    {},
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
	}
	return false
}

func largestNodeByTag(root *html.Node, tag string) *html.Node {
	return largestMatchingNode(root, func(n *html.Node) bool {
		return n.Type == html.ElementNode && strings.EqualFold(n.Data, tag)
	})
}

func largestNodeByRole(root *html.Node, role string) *html.Node {
	return largestMatchingNode(root, func(n *html.Node) bool {
		if n.Type != html.ElementNode {
			return false
		}
		for _, attr := range n.Attr {
			if strings.EqualFold(attr.Key, "role") && strings.EqualFold(strings.TrimSpace(attr.Val), role) {
				return true
			}
		}
		return false
	})
}

func largestMatchingNode(root *html.Node, match func(*html.Node) bool) *html.Node {
	var selected *html.Node
	var selectedLen int
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if match(n) {
			if textLen := markdownTextLen(n); textLen > selectedLen {
				selected = n
				selectedLen = textLen
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return selected
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

func renderHTMLNode(n *html.Node) string {
	var buf bytes.Buffer
	if err := html.Render(&buf, n); err != nil {
		return ""
	}
	return buf.String()
}
