package storage

import (
	neturl "net/url"
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
			if !isSkippableResourceURL(url) && !seen[url] {
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
			if !isSkippableResourceURL(url) && !seen[url] {
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

func isSkippableResourceURL(rawURL string) bool {
	resourceURL := strings.TrimSpace(rawURL)
	resourceURL = strings.Trim(resourceURL, `"'`)
	if resourceURL == "" {
		return true
	}

	return isDataURL(resourceURL) ||
		isFragmentOnlyURL(resourceURL) ||
		hasUnsupportedResourceScheme(resourceURL) ||
		hasMalformedAbsoluteHost(resourceURL)
}

func isDataURL(rawURL string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(rawURL)), "data:")
}

func isFragmentOnlyURL(rawURL string) bool {
	resourceURL := strings.TrimSpace(rawURL)
	resourceURL = strings.Trim(resourceURL, `"'`)
	if strings.HasPrefix(resourceURL, "#") {
		return true
	}

	decodedURL, err := neturl.PathUnescape(resourceURL)
	if err != nil {
		return false
	}

	return strings.HasPrefix(strings.TrimSpace(decodedURL), "#")
}

func hasUnsupportedResourceScheme(rawURL string) bool {
	resourceURL := strings.ToLower(strings.TrimSpace(rawURL))
	return strings.HasPrefix(resourceURL, "blob:") ||
		strings.HasPrefix(resourceURL, "javascript:") ||
		strings.HasPrefix(resourceURL, "mailto:") ||
		strings.HasPrefix(resourceURL, "about:")
}

// hasMalformedAbsoluteHost rejects absolute http(s) URLs whose hostname
// contains characters that can never appear in a valid DNS name. Some
// upstream CSS files ship typos like https://www&google.com/... which would
// otherwise fail DNS lookup and spam the log on every page that uses them.
func hasMalformedAbsoluteHost(rawURL string) bool {
	resourceURL := strings.TrimSpace(rawURL)
	resourceURL = strings.Trim(resourceURL, `"'`)
	lower := strings.ToLower(resourceURL)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return false
	}

	parsed, err := neturl.Parse(resourceURL)
	if err != nil {
		return true
	}
	host := parsed.Hostname()
	if host == "" {
		return true
	}
	for _, r := range host {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-', r == '.', r == '_', r == ':':
			continue
		default:
			if r > 0x7f {
				continue
			}
			return true
		}
	}
	return false
}
