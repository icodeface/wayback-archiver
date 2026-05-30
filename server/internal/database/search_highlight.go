package database

import (
	"html"
	"strings"

	"wayback/internal/models"
)

const searchSnippetContextRunes = 80

func applySearchHighlights(page *models.Page, keyword, bodyText string) {
	page.HighlightedURL = highlightSearchTerm(page.URL, keyword)
	page.HighlightedTitle = highlightSearchTerm(page.Title, keyword)
	page.SearchSnippet = buildSearchSnippet(bodyText, keyword, searchSnippetContextRunes)
}

func buildSearchSnippet(text, keyword string, contextRunes int) string {
	ranges := findFoldRanges(text, keyword)
	if len(ranges) == 0 {
		return ""
	}

	textRunes := []rune(text)
	first := ranges[0]
	start := first.start - contextRunes
	if start < 0 {
		start = 0
	}
	end := first.end + contextRunes
	if end > len(textRunes) {
		end = len(textRunes)
	}

	snippetRunes := textRunes[start:end]
	snippetRanges := make([]searchRange, 0, len(ranges))
	for _, r := range ranges {
		if r.end <= start {
			continue
		}
		if r.start >= end {
			break
		}
		snippetRanges = append(snippetRanges, searchRange{
			start: maxInt(r.start-start, 0),
			end:   minInt(r.end-start, len(snippetRunes)),
		})
	}

	var b strings.Builder
	if start > 0 {
		b.WriteString("...")
	}
	b.WriteString(highlightRunes(snippetRunes, snippetRanges))
	if end < len(textRunes) {
		b.WriteString("...")
	}
	return b.String()
}

func highlightSearchTerm(text, keyword string) string {
	ranges := findFoldRanges(text, keyword)
	if len(ranges) == 0 {
		return ""
	}
	return highlightRunes([]rune(text), ranges)
}

type searchRange struct {
	start int
	end   int
}

func findFoldRanges(text, keyword string) []searchRange {
	textRunes := []rune(text)
	keywordRunes := []rune(keyword)
	if len(keywordRunes) == 0 || len(keywordRunes) > len(textRunes) {
		return nil
	}

	ranges := []searchRange{}
	for i := 0; i <= len(textRunes)-len(keywordRunes); i++ {
		if runesEqualFold(textRunes[i:i+len(keywordRunes)], keywordRunes) {
			ranges = append(ranges, searchRange{start: i, end: i + len(keywordRunes)})
			i += len(keywordRunes) - 1
		}
	}
	return ranges
}

func runesEqualFold(a, b []rune) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !strings.EqualFold(string(a[i]), string(b[i])) {
			return false
		}
	}
	return true
}

func highlightRunes(textRunes []rune, ranges []searchRange) string {
	if len(ranges) == 0 {
		return html.EscapeString(string(textRunes))
	}

	var b strings.Builder
	cursor := 0
	for _, r := range ranges {
		if r.start < cursor || r.start > len(textRunes) || r.end > len(textRunes) || r.start >= r.end {
			continue
		}
		b.WriteString(html.EscapeString(string(textRunes[cursor:r.start])))
		b.WriteString("<mark>")
		b.WriteString(html.EscapeString(string(textRunes[r.start:r.end])))
		b.WriteString("</mark>")
		cursor = r.end
	}
	b.WriteString(html.EscapeString(string(textRunes[cursor:])))
	return b.String()
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
