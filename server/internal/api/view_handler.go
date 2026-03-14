package api

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"wayback/internal/models"
)

// validateResourcePath 验证资源路径，防止目录穿越攻击
// 返回清理后的安全路径，如果路径试图逃逸出 baseDir 则返回错误
func validateResourcePath(baseDir, resourcePath string) (string, error) {
	// 拒绝绝对路径
	if filepath.IsAbs(resourcePath) {
		return "", errors.New("absolute paths are not allowed")
	}

	// 清理路径，移除 ../ 等相对路径元素
	cleanPath := filepath.Clean(resourcePath)

	// 检查清理后的路径是否仍包含 .. （说明试图向上遍历）
	if strings.HasPrefix(cleanPath, "..") || strings.Contains(cleanPath, string(filepath.Separator)+"..") {
		return "", errors.New("path traversal attempt detected")
	}

	// 构建完整路径
	fullPath := filepath.Join(baseDir, cleanPath)

	// 获取绝对路径（解析所有符号链接）
	absFullPath, err := filepath.Abs(fullPath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve absolute path: %w", err)
	}

	absBaseDir, err := filepath.Abs(baseDir)
	if err != nil {
		return "", fmt.Errorf("failed to resolve base directory: %w", err)
	}

	// 检查解析后的路径是否在 baseDir 内
	// 使用 filepath.Rel 检查相对关系
	relPath, err := filepath.Rel(absBaseDir, absFullPath)
	if err != nil {
		return "", fmt.Errorf("failed to compute relative path: %w", err)
	}

	// 如果相对路径以 .. 开头，说明试图逃逸出 baseDir
	if strings.HasPrefix(relPath, "..") || strings.HasPrefix(relPath, string(filepath.Separator)) {
		return "", errors.New("path traversal attempt detected")
	}

	return absFullPath, nil
}

// ViewPage 查看归档页面（静态快照模式，禁用JavaScript）
func (h *Handler) ViewPage(c *gin.Context) {
	id := c.Param("id")
	page, err := h.db.GetPageByID(id)
	if err != nil || page == nil {
		c.String(http.StatusNotFound, "Page not found")
		return
	}

	// 获取快照邻居信息（用于导航）
	prev, next, snapshotTotal, _ := h.db.GetSnapshotNeighbors(page.URL, page.ID)

	// 读取 HTML 文件（已经包含重写后的资源路径）
	htmlPath := filepath.Join(h.dataDir, page.HTMLPath)
	htmlContent, err := os.ReadFile(htmlPath)
	if err != nil {
		c.String(http.StatusInternalServerError, "Failed to read HTML file")
		return
	}

	modifiedHTML := string(htmlContent)

	// 移除 <base> 标签 — 归档页面的资源路径已重写为本地路径，
	// <base href="https://原始域名/"> 会导致浏览器将 /archive/... 解析到原始域名
	baseTagRe := regexp.MustCompile(`(?i)<base\s[^>]*>`)
	modifiedHTML = baseTagRe.ReplaceAllString(modifiedHTML, "")

	// 移除所有 <script> 标签
	modifiedHTML = scriptTagRe.ReplaceAllString(modifiedHTML, "")

	// 移除 <noscript> 标签（CSP 禁用了 JS，浏览器会渲染 noscript 内容，
	// 导致 SPA 页面显示"需要启用 JavaScript"的错误信息和大片空白）
	modifiedHTML = noscriptTagRe.ReplaceAllString(modifiedHTML, "")

	// 移除内联事件处理器
	// 使用两个独立正则分别处理双引号和单引号包裹的属性值，
	// 避免 [^"']* 在遇到嵌套引号时提前终止匹配（如 onclick="window.open('...')"）
	eventHandlerDQ := regexp.MustCompile(`(?i)\s+on\w+\s*=\s*"[^"]*"`)
	eventHandlerSQ := regexp.MustCompile(`(?i)\s+on\w+\s*=\s*'[^']*'`)
	modifiedHTML = eventHandlerDQ.ReplaceAllString(modifiedHTML, "")
	modifiedHTML = eventHandlerSQ.ReplaceAllString(modifiedHTML, "")

	// 移除 javascript: 协议的链接
	jsProtocolRe := regexp.MustCompile(`(?i)href\s*=\s*["']javascript:[^"']*["']`)
	modifiedHTML = jsProtocolRe.ReplaceAllString(modifiedHTML, `href="#"`)

	// 移除 loading="lazy"，归档页面禁用了 JS，懒加载可能无法正常触发
	lazyLoadRe := regexp.MustCompile(`(?i)\s+loading\s*=\s*["']lazy["']`)
	modifiedHTML = lazyLoadRe.ReplaceAllString(modifiedHTML, "")

	// 隐藏 <video> 元素 — 归档不保存视频源文件，空 video 标签会渲染为大黑块
	modifiedHTML = hideVideoElements(modifiedHTML)

	// 修复未重写的 srcset 协议相对 URL（如 srcset="//i1.hdslb.com/..."）
	// 早期归档的页面 srcset 未被重写，在渲染时补偿处理
	modifiedHTML = fixUnrewrittenSrcset(modifiedHTML)

	// 修复嵌套的 <button> 标签（HTML 规范不允许 button 嵌套 button）
	// 浏览器遇到嵌套 button 时会隐式关闭外层 button，破坏 DOM 树结构，
	// 导致后续元素（如 <main>、<section>）被提升到错误的层级
	modifiedHTML = fixNestedButtons(modifiedHTML)

	// 注入归档信息栏（传入 nonce 用于 CSP）
	nonce := "wayback-fix-positioning"
	modifiedHTML = injectArchiveHeader(modifiedHTML, page, prev, next, snapshotTotal, nonce)

	// 设置安全响应头（允许带 nonce 的内联脚本，用于修复定位问题）
	c.Header("X-Frame-Options", "SAMEORIGIN")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("Content-Security-Policy", fmt.Sprintf("default-src 'self'; script-src 'nonce-%s'; img-src * data: blob:; style-src 'self' 'unsafe-inline'; font-src * data:; connect-src 'none'; frame-src 'none'; object-src 'none';", nonce))

	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(modifiedHTML))
}

// ProxyResource 代理资源请求
// 路由格式: /archive/:page_id/:timestamp:modifier/*url
// 例如: /archive/123/20240309150405mp_/https://example.com/style.css
// 或者: /archive/123/20240309150405mp_/resources/xx/yy/hash.css
func (h *Handler) ProxyResource(c *gin.Context) {
	pageID := c.Param("page_id")
	timestamp := c.Param("timestamp")

	// 使用 RawPath 保留 URL 编码（如 %20），回退到 Path
	rawPath := c.Request.URL.RawPath
	if rawPath == "" {
		rawPath = c.Request.URL.Path
	}

	// 从原始路径中提取资源 URL 部分
	// 路径格式: /archive/{page_id}/{timestamp}/{resource_url}
	prefix := fmt.Sprintf("/archive/%s/%s/", pageID, timestamp)
	originalURL := strings.TrimPrefix(rawPath, prefix)

	// 如果有查询参数，也要加上
	if c.Request.URL.RawQuery != "" {
		originalURL = originalURL + "?" + c.Request.URL.RawQuery
	}

	log.Printf("[Proxy] Request: page_id=%s, timestamp=%s, url=%s", pageID, timestamp, originalURL)

	// 检查是否是本地资源路径（以 resources/ 开头）
	if strings.HasPrefix(originalURL, "resources/") {
		// 提取 resources/ 后面的路径部分
		resourcePath := strings.TrimPrefix(originalURL, "resources/")

		// 验证路径，防止目录穿越攻击（确保路径在 resources/ 目录内）
		resourcesDir := filepath.Join(h.dataDir, "resources")
		safePath, err := validateResourcePath(resourcesDir, resourcePath)
		if err != nil {
			log.Printf("[Proxy] Path validation failed for %s: %v", originalURL, err)
			c.String(http.StatusForbidden, "Invalid resource path")
			return
		}

		content, err := os.ReadFile(safePath)
		if err != nil {
			log.Printf("[Proxy] Failed to read local file %s: %v", safePath, err)
			c.String(http.StatusNotFound, "Resource not found")
			return
		}

		// 根据文件扩展名检测 Content-Type
		contentType := detectContentTypeByPath(safePath)
		c.Header("Content-Type", contentType)
		c.Header("Cache-Control", "public, max-age=31536000")

		log.Printf("[Proxy] Serving local file: %s (%s, %d bytes)", originalURL, contentType, len(content))
		c.Data(http.StatusOK, contentType, content)
		return
	}

	// 否则，按原来的逻辑处理（从数据库查询）
	pageIDInt := int64(0)
	fmt.Sscanf(pageID, "%d", &pageIDInt)

	resource, err := h.db.GetResourceByURLAndPageID(originalURL, pageIDInt)
	if err != nil {
		log.Printf("[Proxy] Database error: %v", err)
		c.String(http.StatusInternalServerError, "Database error")
		return
	}

	// 如果精确匹配失败，尝试用 URL 编码后的版本查找（Gin 会解码 %20 等）
	if resource == nil {
		parsed, parseErr := url.Parse(originalURL)
		if parseErr == nil {
			encoded := parsed.String()
			if encoded != originalURL {
				resource, err = h.db.GetResourceByURLAndPageID(encoded, pageIDInt)
				if err != nil {
					log.Printf("[Proxy] Database error (encoded): %v", err)
				}
			}
		}
		// 也尝试手动对路径部分进行编码
		if resource == nil {
			// 将空格替换为 %20 等常见编码
			encodedURL := strings.ReplaceAll(originalURL, " ", "%20")
			if encodedURL != originalURL {
				resource, err = h.db.GetResourceByURLAndPageID(encodedURL, pageIDInt)
				if err != nil {
					log.Printf("[Proxy] Database error (space-encoded): %v", err)
				}
			}
		}
	}

	// 如果仍然找不到，尝试模糊匹配（DB 中的 URL 可能带有 #fragment）
	if resource == nil {
		resource, err = h.db.GetResourceByURLPrefix(originalURL, pageIDInt)
		if err != nil {
			log.Printf("[Proxy] Database error (prefix): %v", err)
		}
	}

	// 如果仍然找不到，尝试按 URL 路径匹配（忽略查询参数，处理同一图片不同 token 的情况）
	if resource == nil {
		urlPath := originalURL
		if idx := strings.IndexByte(urlPath, '?'); idx != -1 {
			urlPath = urlPath[:idx]
		}
		resource, err = h.db.GetResourceByURLPath(urlPath, pageIDInt)
		if err != nil {
			log.Printf("[Proxy] Database error (path): %v", err)
		}
	}

	if resource == nil {
		log.Printf("[Proxy] Resource not found: %s", originalURL)
		c.String(http.StatusNotFound, "Resource not found")
		return
	}

	// 读取文件
	filePath := filepath.Join(h.dataDir, resource.FilePath)
	content, err := os.ReadFile(filePath)
	if err != nil {
		log.Printf("[Proxy] Failed to read file %s: %v", filePath, err)
		c.String(http.StatusInternalServerError, "Failed to read file")
		return
	}

	// 设置Content-Type
	contentType := detectContentType(resource)
	c.Header("Content-Type", contentType)
	c.Header("Cache-Control", "public, max-age=31536000")

	log.Printf("[Proxy] Serving: %s (%s, %d bytes)", originalURL, contentType, len(content))
	c.Data(http.StatusOK, contentType, content)
}

// detectContentType 检测资源的Content-Type
func detectContentType(resource *models.Resource) string {
	switch resource.ResourceType {
	case "css":
		return "text/css; charset=utf-8"
	case "js":
		return "application/javascript; charset=utf-8"
	case "image":
		// 优先使用原始 URL 判断图片类型（存储路径统一为 .img，无法区分）
		if ct := detectImageType(resource.URL); ct != "image/jpeg" {
			return ct
		}
		return detectImageType(resource.FilePath)
	case "font":
		return detectFontType(resource.FilePath)
	default:
		// 尝试从 URL 路径扩展名推断（如 .jpg、.png）
		if ct := detectImageType(resource.URL); ct != "image/jpeg" {
			return ct
		}
		// 尝试从 URL 查询参数推断（如 ?format=jpg）
		if ct := detectContentTypeFromQuery(resource.URL); ct != "" {
			return ct
		}
		// 尝试从 URL 路径推断常见类型
		if ct := detectContentTypeByURL(resource.URL); ct != "" {
			return ct
		}
		return "application/octet-stream"
	}
}

// detectContentTypeByPath 根据文件路径检测 Content-Type
func detectContentTypeByPath(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".css":
		return "text/css; charset=utf-8"
	case ".js":
		return "application/javascript; charset=utf-8"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".svg":
		return "image/svg+xml"
	case ".ico":
		return "image/x-icon"
	case ".woff":
		return "font/woff"
	case ".woff2":
		return "font/woff2"
	case ".ttf":
		return "font/ttf"
	case ".otf":
		return "font/otf"
	case ".eot":
		return "application/vnd.ms-fontobject"
	case ".mp4":
		return "video/mp4"
	case ".mov":
		return "video/quicktime"
	case ".webm":
		return "video/webm"
	case ".mp3":
		return "audio/mpeg"
	default:
		return "application/octet-stream"
	}
}

// detectImageType 检测图片类型
func detectImageType(filePath string) string {
	// 去掉查询参数（URL 可能带 ?v=xxx）
	if idx := strings.IndexByte(filePath, '?'); idx != -1 {
		filePath = filePath[:idx]
	}
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".svg":
		return "image/svg+xml"
	case ".ico":
		return "image/x-icon"
	default:
		return "image/jpeg"
	}
}

// detectContentTypeFromQuery 从 URL 查询参数推断 Content-Type（如 ?format=jpg）
func detectContentTypeFromQuery(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	format := strings.ToLower(parsed.Query().Get("format"))
	switch format {
	case "jpg", "jpeg":
		return "image/jpeg"
	case "png":
		return "image/png"
	case "gif":
		return "image/gif"
	case "webp":
		return "image/webp"
	case "svg":
		return "image/svg+xml"
	default:
		return ""
	}
}

// detectContentTypeByURL 从 URL 路径扩展名推断 Content-Type
func detectContentTypeByURL(rawURL string) string {
	// 去掉查询参数
	u := rawURL
	if idx := strings.IndexByte(u, '?'); idx != -1 {
		u = u[:idx]
	}
	ext := strings.ToLower(filepath.Ext(u))
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".svg":
		return "image/svg+xml"
	case ".mp4":
		return "video/mp4"
	case ".mov":
		return "video/quicktime"
	case ".webm":
		return "video/webm"
	case ".mp3":
		return "audio/mpeg"
	case ".woff":
		return "font/woff"
	case ".woff2":
		return "font/woff2"
	case ".ttf":
		return "font/ttf"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js":
		return "application/javascript; charset=utf-8"
	default:
		return ""
	}
}

// fixNestedButtons 修复嵌套的 <button> 标签
// HTML 规范不允许 <button> 内嵌套 <button>，浏览器会隐式关闭外层 button，
// 导致后续 DOM 结构被破坏（如 <main>、<section> 被提升到错误层级）。
// 将嵌套的内层 <button> 替换为 <span>，保留样式和内容。
func fixNestedButtons(html string) string {
	var result strings.Builder
	result.Grow(len(html))

	buttonDepth := 0
	i := 0

	for i < len(html) {
		if i+7 <= len(html) && strings.EqualFold(html[i:i+7], "<button") && (i+7 == len(html) || html[i+7] == ' ' || html[i+7] == '>' || html[i+7] == '/') {
			if buttonDepth > 0 {
				result.WriteString("<span")
				i += 7
			} else {
				result.WriteString("<button")
				i += 7
			}
			buttonDepth++
			continue
		}
		if i+9 <= len(html) && strings.EqualFold(html[i:i+9], "</button>") {
			buttonDepth--
			if buttonDepth > 0 {
				result.WriteString("</span>")
			} else {
				result.WriteString("</button>")
			}
			if buttonDepth < 0 {
				buttonDepth = 0
			}
			i += 9
			continue
		}
		result.WriteByte(html[i])
		i++
	}

	return result.String()
}

// ServeLocalResource 直接提供本地资源文件（CSS 中引用的资源）
// 路由格式: /archive/resources/*filepath
func (h *Handler) ServeLocalResource(c *gin.Context) {
	resourcePath := strings.TrimPrefix(c.Param("filepath"), "/")

	// 验证路径，防止目录穿越攻击
	resourcesDir := filepath.Join(h.dataDir, "resources")
	safePath, err := validateResourcePath(resourcesDir, resourcePath)
	if err != nil {
		log.Printf("[ServeLocalResource] Path validation failed for %s: %v", resourcePath, err)
		c.String(http.StatusForbidden, "Invalid resource path")
		return
	}

	content, err := os.ReadFile(safePath)
	if err != nil {
		c.String(http.StatusNotFound, "Resource not found")
		return
	}

	contentType := detectContentTypeByPath(safePath)
	c.Header("Content-Type", contentType)
	c.Header("Cache-Control", "public, max-age=31536000")
	c.Data(http.StatusOK, contentType, content)
}

// fixUnrewrittenSrcset 修复未重写的 srcset 协议相对 URL
// 早期归档的页面 srcset 属性未被重写，这里在渲染时补偿处理
// 策略：删除 <picture> 中的 <source> 元素，让浏览器回退到 <img> 标签（已被正确重写）
func fixUnrewrittenSrcset(html string) string {
	// 删除 <picture> 标签内的 <source> 元素
	// <source> 提供 avif/webp 等现代格式，但 srcset 未重写会导致加载失败
	// <img> 的 src 已被正确重写，删除 <source> 后浏览器会回退到 <img>
	sourceTagRe := regexp.MustCompile(`(?is)<source[^>]*>`)
	html = sourceTagRe.ReplaceAllString(html, "")

	return html
}

// hideVideoElements 隐藏没有任何视频源的 video 元素
// 只隐藏既没有 src 属性、内部也没有 <source> 子元素的 video（空壳会渲染为黑块）
func hideVideoElements(html string) string {
	// 匹配完整的 <video>...</video> 或自闭合 <video/>
	videoBlockRe := regexp.MustCompile(`(?is)<video\b([^>]*)>(.*?)</video>|<video\b([^>]*)/>`)
	html = videoBlockRe.ReplaceAllStringFunc(html, func(match string) string {
		// 有 src 属性 → 保留（不管是本地还是外部）
		if srcAttrRe.MatchString(match) {
			return match
		}
		// 内部有 <source> 子元素 → 保留
		if strings.Contains(strings.ToLower(match), "<source") {
			return match
		}
		// 无源的空 video → 隐藏
		return strings.Replace(match, "<video", `<video style="display:none!important"`, 1)
	})
	return html
}