package storage

import (
	htmlpkg "html"
	"log"
	"net/url"
	"regexp"
	"strings"
)

// HTMLResourceExtractor 从 HTML 中提取资源 URL
type HTMLResourceExtractor struct{}

func NewHTMLResourceExtractor() *HTMLResourceExtractor {
	return &HTMLResourceExtractor{}
}

// ExtractResources 从 HTML 中提取所有外部资源 URL
func (e *HTMLResourceExtractor) ExtractResources(html string, pageURL string) []ResourceRef {
	resources := make(map[string]ResourceRef)

	// 提取 <img src="...">（使用 \ssrc= 避免匹配 data-src=）
	imgRegex := regexp.MustCompile(`<img[^>]*\ssrc=["']([^"']+)["']`)
	matches := imgRegex.FindAllStringSubmatch(html, -1)
	for _, match := range matches {
		if len(match) > 1 {
			rawURL := htmlpkg.UnescapeString(match[1])
			fullURL := e.resolveURL(rawURL, pageURL)
			if e.isExternalURL(fullURL) {
				resources[fullURL] = ResourceRef{URL: fullURL, Type: "image"}
			}
		}
	}

	// 提取 <link rel="stylesheet" href="...">
	cssRegex := regexp.MustCompile(`<link[^>]+rel=["']stylesheet["'][^>]+href=["']([^"']+)["']`)
	matches = cssRegex.FindAllStringSubmatch(html, -1)
	for _, match := range matches {
		if len(match) > 1 {
			rawURL := htmlpkg.UnescapeString(match[1])
			fullURL := e.resolveURL(rawURL, pageURL)
			if e.isExternalURL(fullURL) {
				resources[fullURL] = ResourceRef{URL: fullURL, Type: "css"}
			}
		}
	}

	// 提取 <link href="..." rel="stylesheet">（顺序相反）
	cssRegex2 := regexp.MustCompile(`<link[^>]+href=["']([^"']+)["'][^>]+rel=["']stylesheet["']`)
	matches = cssRegex2.FindAllStringSubmatch(html, -1)
	for _, match := range matches {
		if len(match) > 1 {
			rawURL := htmlpkg.UnescapeString(match[1])
			fullURL := e.resolveURL(rawURL, pageURL)
			if e.isExternalURL(fullURL) {
				resources[fullURL] = ResourceRef{URL: fullURL, Type: "css"}
			}
		}
	}

	// 提取 <script src="...">
	jsRegex := regexp.MustCompile(`<script[^>]+src=["']([^"']+)["']`)
	matches = jsRegex.FindAllStringSubmatch(html, -1)
	for _, match := range matches {
		if len(match) > 1 {
			rawURL := htmlpkg.UnescapeString(match[1])
			fullURL := e.resolveURL(rawURL, pageURL)
			if e.isExternalURL(fullURL) {
				resources[fullURL] = ResourceRef{URL: fullURL, Type: "js"}
			}
		}
	}

	// 提取 <link rel="preload" as="font" href="..."> 和 <link rel="preload" href="..." as="font">
	fontRegex := regexp.MustCompile(`<link[^>]+rel=["']preload["'][^>]+as=["']font["'][^>]+href=["']([^"']+)["']`)
	matches = fontRegex.FindAllStringSubmatch(html, -1)
	for _, match := range matches {
		if len(match) > 1 {
			rawURL := htmlpkg.UnescapeString(match[1])
			fullURL := e.resolveURL(rawURL, pageURL)
			if e.isExternalURL(fullURL) {
				resources[fullURL] = ResourceRef{URL: fullURL, Type: "font"}
			}
		}
	}

	// 提取 <link rel="preload" href="..." as="font">（顺序相反）
	fontRegex2 := regexp.MustCompile(`<link[^>]+rel=["']preload["'][^>]+href=["']([^"']+)["'][^>]+as=["']font["']`)
	matches = fontRegex2.FindAllStringSubmatch(html, -1)
	for _, match := range matches {
		if len(match) > 1 {
			rawURL := htmlpkg.UnescapeString(match[1])
			fullURL := e.resolveURL(rawURL, pageURL)
			if e.isExternalURL(fullURL) {
				resources[fullURL] = ResourceRef{URL: fullURL, Type: "font"}
			}
		}
	}

	// 提取 <link rel="icon" href="...">
	iconRegex := regexp.MustCompile(`<link[^>]+rel=["']icon["'][^>]+href=["']([^"']+)["']`)
	matches = iconRegex.FindAllStringSubmatch(html, -1)
	for _, match := range matches {
		if len(match) > 1 {
			rawURL := htmlpkg.UnescapeString(match[1])
			fullURL := e.resolveURL(rawURL, pageURL)
			if e.isExternalURL(fullURL) {
				resources[fullURL] = ResourceRef{URL: fullURL, Type: "image"}
			}
		}
	}

	// 提取 <link href="..." rel="icon">（顺序相反）
	iconRegex2 := regexp.MustCompile(`<link[^>]+href=["']([^"']+)["'][^>]+rel=["']icon["']`)
	matches = iconRegex2.FindAllStringSubmatch(html, -1)
	for _, match := range matches {
		if len(match) > 1 {
			rawURL := htmlpkg.UnescapeString(match[1])
			fullURL := e.resolveURL(rawURL, pageURL)
			if e.isExternalURL(fullURL) {
				resources[fullURL] = ResourceRef{URL: fullURL, Type: "image"}
			}
		}
	}

	// 提取 srcset 属性中的图片 URL
	srcsetRegex := regexp.MustCompile(`srcset=["']([^"']+)["']`)
	matches = srcsetRegex.FindAllStringSubmatch(html, -1)
	for _, match := range matches {
		if len(match) > 1 {
			srcsetURLs := parseSrcset(htmlpkg.UnescapeString(match[1]))
			for _, rawURL := range srcsetURLs {
				fullURL := e.resolveURL(rawURL, pageURL)
				if e.isExternalURL(fullURL) {
					resources[fullURL] = ResourceRef{URL: fullURL, Type: "image"}
				}
			}
		}
	}

	// 提取 <video poster="..."> 和 <video src="...">
	videoPosterRegex := regexp.MustCompile(`<video[^>]+poster=["']([^"']+)["']`)
	matches = videoPosterRegex.FindAllStringSubmatch(html, -1)
	for _, match := range matches {
		if len(match) > 1 {
			fullURL := e.resolveURL(htmlpkg.UnescapeString(match[1]), pageURL)
			if e.isExternalURL(fullURL) {
				resources[fullURL] = ResourceRef{URL: fullURL, Type: "image"}
			}
		}
	}

	videoSrcRegex := regexp.MustCompile(`<video[^>]+src=["']([^"']+)["']`)
	matches = videoSrcRegex.FindAllStringSubmatch(html, -1)
	for _, match := range matches {
		if len(match) > 1 {
			fullURL := e.resolveURL(htmlpkg.UnescapeString(match[1]), pageURL)
			if e.isExternalURL(fullURL) {
				resources[fullURL] = ResourceRef{URL: fullURL, Type: "other"}
			}
		}
	}

	// 提取 <audio src="...">
	audioSrcRegex := regexp.MustCompile(`<audio[^>]+src=["']([^"']+)["']`)
	matches = audioSrcRegex.FindAllStringSubmatch(html, -1)
	for _, match := range matches {
		if len(match) > 1 {
			fullURL := e.resolveURL(htmlpkg.UnescapeString(match[1]), pageURL)
			if e.isExternalURL(fullURL) {
				resources[fullURL] = ResourceRef{URL: fullURL, Type: "other"}
			}
		}
	}

	// 提取 <source src="...">（video/audio 子元素）
	sourceSrcRegex := regexp.MustCompile(`<source[^>]+src=["']([^"']+)["']`)
	matches = sourceSrcRegex.FindAllStringSubmatch(html, -1)
	for _, match := range matches {
		if len(match) > 1 {
			fullURL := e.resolveURL(htmlpkg.UnescapeString(match[1]), pageURL)
			if e.isExternalURL(fullURL) {
				resources[fullURL] = ResourceRef{URL: fullURL, Type: "other"}
			}
		}
	}

	// 提取 CSS 中的 url(...)
	cssUrlRegex := regexp.MustCompile(`url\(["']?([^"')]+)["']?\)`)
	matches = cssUrlRegex.FindAllStringSubmatch(html, -1)
	for _, match := range matches {
		if len(match) > 1 {
			rawURL := match[1]
			// 解码HTML实体（如 &quot; -> "）
			rawURL = htmlpkg.UnescapeString(rawURL)
			// 移除可能残留的引号
			rawURL = strings.Trim(rawURL, `"'`)
			fullURL := e.resolveURL(rawURL, pageURL)
			if e.isExternalURL(fullURL) {
				// 猜测类型
				resourceType := guessResourceType(fullURL)
				resources[fullURL] = ResourceRef{URL: fullURL, Type: resourceType}
			}
		}
	}

	// 提取 url(&quot;...&quot;) 格式（HTML 实体编码的引号）
	quotUrlRegex := regexp.MustCompile(`url\(&quot;(.+?)&quot;\)`)
	matches = quotUrlRegex.FindAllStringSubmatch(html, -1)
	for _, match := range matches {
		if len(match) > 1 {
			rawURL := htmlpkg.UnescapeString(match[1])
			rawURL = strings.Trim(rawURL, `"'`)
			fullURL := e.resolveURL(rawURL, pageURL)
			if e.isExternalURL(fullURL) {
				resourceType := guessResourceType(fullURL)
				resources[fullURL] = ResourceRef{URL: fullURL, Type: resourceType}
			}
		}
	}

	// 转换为切片
	result := make([]ResourceRef, 0, len(resources))
	for _, res := range resources {
		result = append(result, res)
	}

	log.Printf("Extracted %d external resources from HTML", len(result))
	return result
}

// isExternalURL 判断是否是外部 URL
func (e *HTMLResourceExtractor) isExternalURL(url string) bool {
	// 跳过 data: URLs
	if strings.HasPrefix(url, "data:") {
		return false
	}

	// 跳过相对路径（这些应该由浏览器端处理）
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return false
	}

	// 跳过本地 URL
	if strings.Contains(url, "localhost") || strings.Contains(url, "127.0.0.1") {
		return false
	}

	return true
}

// resolveURL 将相对URL转换为绝对URL，并规范化路径（处理 ../ 等）
func (e *HTMLResourceExtractor) resolveURL(rawURL, baseURL string) string {
	// 跳过 data: URLs
	if strings.HasPrefix(rawURL, "data:") {
		return rawURL
	}

	// 如果已经是完整URL，解析并规范化路径
	if strings.HasPrefix(rawURL, "http://") || strings.HasPrefix(rawURL, "https://") {
		parsed, err := url.Parse(rawURL)
		if err != nil {
			log.Printf("Failed to parse absolute URL %s: %v", rawURL, err)
			return rawURL
		}
		// 使用 ResolveReference 规范化路径（处理 ../ 等）
		normalized := parsed.ResolveReference(&url.URL{})
		return normalized.String()
	}

	// 解析基础URL
	base, err := url.Parse(baseURL)
	if err != nil {
		log.Printf("Failed to parse base URL %s: %v", baseURL, err)
		return rawURL
	}

	// 解析相对URL
	ref, err := url.Parse(rawURL)
	if err != nil {
		log.Printf("Failed to parse relative URL %s: %v", rawURL, err)
		return rawURL
	}

	// 合并URL（自动规范化路径）
	resolved := base.ResolveReference(ref)
	return resolved.String()
}

// ResourceRef 资源引用
type ResourceRef struct {
	URL  string
	Type string
}

// guessResourceType 根据 URL 猜测资源类型
func guessResourceType(fullURL string) string {
	lower := strings.ToLower(fullURL)
	if strings.Contains(lower, ".woff") ||
		strings.Contains(lower, ".ttf") ||
		strings.Contains(lower, ".otf") ||
		strings.Contains(lower, ".eot") {
		return "font"
	}
	if strings.Contains(lower, ".png") ||
		strings.Contains(lower, ".jpg") ||
		strings.Contains(lower, ".jpeg") ||
		strings.Contains(lower, ".gif") ||
		strings.Contains(lower, ".svg") ||
		strings.Contains(lower, ".webp") {
		return "image"
	}
	return "other"
}

// parseSrcset 解析 srcset 属性值，返回 URL 列表
func parseSrcset(srcset string) []string {
	var urls []string
	parts := strings.Split(srcset, ",")
	for _, part := range parts {
		fields := strings.Fields(strings.TrimSpace(part))
		if len(fields) > 0 && fields[0] != "" {
			urls = append(urls, fields[0])
		}
	}
	return urls
}
