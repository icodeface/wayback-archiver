package storage

import (
	"regexp"
	"strings"
)

// CSSParser extracts resource URLs from CSS content
type CSSParser struct {
	// Regex patterns for CSS resource references
	importPattern *regexp.Regexp
	urlPattern    *regexp.Regexp
}

func NewCSSParser() *CSSParser {
	return &CSSParser{
		// Match @import statements: @import url("...") or @import "..."
		importPattern: regexp.MustCompile(`@import\s+(?:url\()?["']?([^"')]+)["']?\)?`),
		// Match url() references: url("...") or url('...') or url(...)
		urlPattern: regexp.MustCompile(`url\(["']?([^"')]+)["']?\)`),
	}
}

// ExtractResources extracts all resource URLs from CSS content
func (p *CSSParser) ExtractResources(cssContent string) []string {
	seen := make(map[string]bool)
	var resources []string

	// Extract @import URLs
	importMatches := p.importPattern.FindAllStringSubmatch(cssContent, -1)
	for _, match := range importMatches {
		if len(match) > 1 {
			url := strings.TrimSpace(match[1])
			if url != "" && !seen[url] && !isDataURL(url) {
				seen[url] = true
				resources = append(resources, url)
			}
		}
	}

	// Extract url() references
	urlMatches := p.urlPattern.FindAllStringSubmatch(cssContent, -1)
	for _, match := range urlMatches {
		if len(match) > 1 {
			url := strings.TrimSpace(match[1])
			if url != "" && !seen[url] && !isDataURL(url) {
				seen[url] = true
				resources = append(resources, url)
			}
		}
	}

	return resources
}

// RewriteCSS rewrites resource URLs in CSS content to point to local paths
func (p *CSSParser) RewriteCSS(cssContent string, urlMapping map[string]string) string {
	result := cssContent

	// Rewrite @import URLs
	result = p.importPattern.ReplaceAllStringFunc(result, func(match string) string {
		submatch := p.importPattern.FindStringSubmatch(match)
		if len(submatch) > 1 {
			originalURL := strings.TrimSpace(submatch[1])
			if localPath, ok := urlMapping[originalURL]; ok {
				localURL := "/archive/" + localPath
				// Preserve the original format
				if strings.Contains(match, "url(") {
					return strings.Replace(match, originalURL, localURL, 1)
				}
				return strings.Replace(match, originalURL, localURL, 1)
			}
		}
		return match
	})

	// Rewrite url() references
	result = p.urlPattern.ReplaceAllStringFunc(result, func(match string) string {
		submatch := p.urlPattern.FindStringSubmatch(match)
		if len(submatch) > 1 {
			originalURL := strings.TrimSpace(submatch[1])
			if localPath, ok := urlMapping[originalURL]; ok {
				localURL := "/archive/" + localPath
				// Preserve quotes if present
				if strings.Contains(match, `"`) {
					return `url("` + localURL + `")`
				} else if strings.Contains(match, `'`) {
					return `url('` + localURL + `')`
				}
				return `url(` + localURL + `)`
			}
		}
		return match
	})

	return result
}

// isDataURL checks if a URL is a data URL (data:...)
func isDataURL(url string) bool {
	return strings.HasPrefix(url, "data:")
}
