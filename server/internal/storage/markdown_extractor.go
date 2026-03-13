package storage

import (
	"regexp"
	"strings"

	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/strikethrough"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/table"
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
	// 先移除非正文标签内容，减少噪音
	htmlContent = removeNonContentTags(htmlContent)

	markdown, err := mdConverter.ConvertString(htmlContent)
	if err != nil {
		return ""
	}

	// 合并连续空行
	markdown = multiNewlineRe.ReplaceAllString(markdown, "\n\n")
	return strings.TrimSpace(markdown) + "\n"
}

// removeNonContentTags 移除非正文 HTML 标签及其内容
func removeNonContentTags(html string) string {
	for _, tag := range []string{"script", "style", "noscript", "svg", "nav", "footer", "iframe", "object", "embed"} {
		re := regexp.MustCompile(`(?is)<` + tag + `\b[^>]*>.*?</` + tag + `>`)
		html = re.ReplaceAllString(html, "")
		re2 := regexp.MustCompile(`(?i)<` + tag + `\b[^>]*/?>`)
		html = re2.ReplaceAllString(html, "")
	}
	return html
}
