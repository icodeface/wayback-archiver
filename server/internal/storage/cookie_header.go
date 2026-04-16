package storage

import (
	"net/url"
	"sort"
	"strings"
	"time"

	"wayback/internal/models"
)

type matchedCookie struct {
	name  string
	value string
	path  string
}

func buildCookieHeader(resourceURL, pageURL string, cookies []models.CaptureCookie) string {
	return buildCookieHeaderAt(resourceURL, pageURL, cookies, time.Now())
}

func buildCookieHeaderAt(resourceURL, pageURL string, cookies []models.CaptureCookie, now time.Time) string {
	if len(cookies) == 0 {
		return ""
	}

	resourceParsed, err := url.Parse(resourceURL)
	if err != nil || resourceParsed.Hostname() == "" {
		return ""
	}

	requestHost := normalizeHostname(resourceParsed.Hostname())
	requestPath := resourceParsed.EscapedPath()
	if requestPath == "" {
		requestPath = "/"
	}

	matched := make([]matchedCookie, 0, len(cookies))
	for _, cookie := range cookies {
		if !cookieMatchesRequest(cookie, requestHost, requestPath, resourceParsed.Scheme, resourceURL, pageURL, now) {
			continue
		}
		matched = append(matched, matchedCookie{
			name:  cookie.Name,
			value: cookie.Value,
			path:  normalizedCookiePath(cookie.Path),
		})
	}

	if len(matched) == 0 {
		return ""
	}

	// Browsers send more specific paths first. We do not have creation-time
	// metadata, so keep the remaining order deterministic by cookie name.
	sort.SliceStable(matched, func(i, j int) bool {
		if len(matched[i].path) != len(matched[j].path) {
			return len(matched[i].path) > len(matched[j].path)
		}
		return matched[i].name < matched[j].name
	})

	parts := make([]string, 0, len(matched))
	for _, cookie := range matched {
		parts = append(parts, cookie.name+"="+cookie.value)
	}
	return strings.Join(parts, "; ")
}

func cookieMatchesRequest(cookie models.CaptureCookie, requestHost, requestPath, requestScheme, resourceURL, pageURL string, now time.Time) bool {
	if cookie.Name == "" || cookie.Domain == "" {
		return false
	}
	if !cookie.Session && cookie.ExpirationDate > 0 && cookie.ExpirationDate <= float64(now.Unix()) {
		return false
	}
	if cookie.Secure && !strings.EqualFold(requestScheme, "https") {
		return false
	}
	if !cookieDomainMatches(cookie, requestHost) {
		return false
	}
	if !cookiePathMatches(normalizedCookiePath(cookie.Path), requestPath) {
		return false
	}
	if !cookiePartitionMatches(cookie, pageURL) {
		return false
	}
	if !cookieSameSiteAllows(cookie, pageURL, resourceURL) {
		return false
	}
	return true
}

func cookieDomainMatches(cookie models.CaptureCookie, requestHost string) bool {
	cookieDomain := normalizeCookieDomain(cookie.Domain)
	if cookieDomain == "" || requestHost == "" {
		return false
	}
	if cookie.HostOnly {
		return requestHost == cookieDomain
	}
	if requestHost == cookieDomain {
		return true
	}
	return strings.HasSuffix(requestHost, "."+cookieDomain)
}

func cookiePathMatches(cookiePath, requestPath string) bool {
	if cookiePath == requestPath {
		return true
	}
	if !strings.HasPrefix(requestPath, cookiePath) {
		return false
	}
	if strings.HasSuffix(cookiePath, "/") {
		return true
	}
	if len(requestPath) == len(cookiePath) {
		return true
	}
	return requestPath[len(cookiePath)] == '/'
}

func cookieSameSiteAllows(cookie models.CaptureCookie, pageURL, resourceURL string) bool {
	sameSite := strings.ToLower(strings.TrimSpace(cookie.SameSite))
	switch sameSite {
	case "strict", "lax":
		return isSchemefulSameSite(pageURL, resourceURL)
	default:
		return true
	}
}

func cookiePartitionMatches(cookie models.CaptureCookie, pageURL string) bool {
	if strings.TrimSpace(cookie.PartitionTopLevelSite) == "" {
		return true
	}
	pageSite, ok := canonicalSiteForCookies(pageURL)
	if !ok {
		return false
	}
	partitionSite, ok := canonicalSiteForCookies(cookie.PartitionTopLevelSite)
	if !ok {
		return false
	}
	return pageSite == partitionSite
}

func isSchemefulSameSite(url1, url2 string) bool {
	site1, ok1 := canonicalSiteForCookies(url1)
	site2, ok2 := canonicalSiteForCookies(url2)
	return ok1 && ok2 && site1 == site2
}

func canonicalSiteForCookies(raw string) (string, bool) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Hostname() == "" {
		return "", false
	}
	host := normalizeHostname(parsed.Hostname())
	if host == "" {
		return "", false
	}
	return strings.ToLower(parsed.Scheme) + "://" + getRootDomain(host), true
}

func normalizeCookieDomain(domain string) string {
	domain = normalizeHostname(domain)
	return strings.TrimPrefix(domain, ".")
}

func normalizeHostname(host string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
}

func normalizedCookiePath(path string) string {
	if path == "" || path[0] != '/' {
		return "/"
	}
	return path
}
