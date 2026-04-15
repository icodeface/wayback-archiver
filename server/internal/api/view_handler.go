package api

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	htmlpkg "html"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"wayback/internal/models"
)

// generateNonce 生成随机 CSP nonce（128 位，base64 编码）
func generateNonce() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// 降级到时间戳（不应该发生，但提供回退）
		return fmt.Sprintf("nonce-%d", time.Now().UnixNano())
	}
	return base64.StdEncoding.EncodeToString(b)
}

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
	pageID, ok := parsePageIDParam(c)
	if !ok {
		return
	}

	page, err := h.db.GetPageByID(strconv.FormatInt(pageID, 10))
	if err != nil {
		c.String(http.StatusInternalServerError, "Database error")
		return
	}
	if page == nil {
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

	modifiedHTML := sanitizeArchivedHTML(string(htmlContent))

	// 注入归档信息栏（传入 nonce 用于 CSP）
	// 生成随机 nonce，防止归档页面中的恶意脚本绕过 CSP
	nonce := generateNonce()
	modifiedHTML = injectArchiveHeader(modifiedHTML, page, prev, next, snapshotTotal, nonce)

	// 设置安全响应头（允许带 nonce 的内联脚本，用于修复定位问题）
	c.Header("X-Frame-Options", "SAMEORIGIN")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("Content-Security-Policy", fmt.Sprintf("default-src 'self'; script-src 'nonce-%s'; img-src * data: blob:; style-src 'self' 'unsafe-inline'; font-src * data:; connect-src 'none'; frame-src 'self'; object-src 'none';", nonce))

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

		// 流式响应，避免将整个文件加载到内存
		serveFileStreaming(c, safePath)
		return
	}

	// 否则，按原来的逻辑处理（从数据库查询）
	pageIDInt, err := strconv.ParseInt(pageID, 10, 64)
	if err != nil || pageIDInt <= 0 {
		c.String(http.StatusBadRequest, "Invalid page ID")
		return
	}

	resource, err := h.findResourceForPage(originalURL, pageIDInt)
	if err != nil {
		log.Printf("[Proxy] Database error: %v", err)
		c.String(http.StatusInternalServerError, "Database error")
		return
	}

	if resource == nil {
		log.Printf("[Proxy] Resource not found: %s", originalURL)
		c.String(http.StatusNotFound, "Resource not found")
		return
	}

	if resource.ResourceType == "css" {
		h.serveRewrittenCSS(c, pageIDInt, resource)
		return
	}
	if h.shouldServeArchivedHTML(c, resource) {
		h.serveArchivedHTMLResource(c, resource)
		return
	}

	// 读取文件
	filePath := filepath.Join(h.dataDir, resource.FilePath)

	// 设置Content-Type（需要在 serveFileStreaming 之前，因为 ServeContent 会用文件扩展名推断）
	contentType := detectContentType(resource)
	c.Header("Content-Type", contentType)
	c.Header("Cache-Control", "public, max-age=31536000")

	// 流式响应，避免将整个文件加载到内存
	f, err := os.Open(filePath)
	if err != nil {
		log.Printf("[Proxy] Failed to read file %s: %v", filePath, err)
		c.String(http.StatusInternalServerError, "Failed to read file")
		return
	}
	defer f.Close()
	stat, _ := f.Stat()
	http.ServeContent(c.Writer, c.Request, filepath.Base(filePath), stat.ModTime(), f)
}

func (h *Handler) serveRewrittenCSS(c *gin.Context, pageID int64, resource *models.Resource) {
	filePath := filepath.Join(h.dataDir, resource.FilePath)
	cssContent, err := os.ReadFile(filePath)
	if err != nil {
		log.Printf("[Proxy] Failed to read CSS file %s: %v", filePath, err)
		c.String(http.StatusInternalServerError, "Failed to read file")
		return
	}

	rewritten := rewriteCSSForPage(h.cssParser(), string(cssContent), resource.URL, func(resourceURL string) (string, bool) {
		cssResource, findErr := h.findResourceForPageOnly(resourceURL, pageID)
		if findErr != nil {
			log.Printf("[Proxy] Failed to resolve CSS sub-resource %s: %v", resourceURL, findErr)
			return "", false
		}
		if cssResource == nil {
			return "", false
		}
		return cssResource.FilePath, true
	})

	c.Header("Content-Type", "text/css; charset=utf-8")
	c.Header("Cache-Control", "public, max-age=31536000")
	c.Data(http.StatusOK, "text/css; charset=utf-8", []byte(rewritten))
}

func (h *Handler) serveArchivedHTMLResource(c *gin.Context, resource *models.Resource) {
	filePath := filepath.Join(h.dataDir, resource.FilePath)
	htmlContent, err := os.ReadFile(filePath)
	if err != nil {
		log.Printf("[Proxy] Failed to read HTML file %s: %v", filePath, err)
		c.String(http.StatusInternalServerError, "Failed to read file")
		return
	}

	sanitized := sanitizeArchivedHTML(string(htmlContent))
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Header("Cache-Control", "public, max-age=31536000")
	c.Header("Content-Security-Policy", "default-src 'self'; script-src 'none'; img-src * data: blob:; style-src 'self' 'unsafe-inline'; font-src * data:; connect-src 'none'; frame-src 'self'; object-src 'none';")
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(sanitized))
}

func (h *Handler) shouldServeArchivedHTML(c *gin.Context, resource *models.Resource) bool {
	if resource.ResourceType == "html" {
		return true
	}

	fetchDest := strings.ToLower(strings.TrimSpace(c.GetHeader("Sec-Fetch-Dest")))
	switch fetchDest {
	case "document", "iframe", "frame":
		return true
	}

	accept := strings.ToLower(c.GetHeader("Accept"))
	if strings.Contains(accept, "text/html") {
		if looksLikeHTMLURL(resource.URL) {
			return true
		}
		return resourceLooksLikeHTML(filepath.Join(h.dataDir, resource.FilePath))
	}

	return false
}

func looksLikeHTMLURL(rawURL string) bool {
	lower := strings.ToLower(rawURL)
	if strings.Contains(lower, ".html") || strings.Contains(lower, ".htm") || strings.Contains(lower, "/html/") {
		return true
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}

	pathLower := strings.ToLower(parsed.Path)
	queryLower := strings.ToLower(parsed.RawQuery)
	if strings.Contains(pathLower, "/cgi-bin/") && (strings.Contains(pathLower, "html") || strings.Contains(queryLower, "html") || strings.Contains(queryLower, "iframe")) {
		return true
	}

	return false
}

func resourceLooksLikeHTML(filePath string) bool {
	f, err := os.Open(filePath)
	if err != nil {
		return false
	}
	defer f.Close()

	buf := make([]byte, 512)
	n, err := f.Read(buf)
	if err != nil || n == 0 {
		return false
	}

	sample := strings.ToLower(string(bytes.TrimSpace(buf[:n])))
	return strings.HasPrefix(sample, "<!doctype html") || strings.HasPrefix(sample, "<html") || strings.HasPrefix(sample, "<head") || strings.HasPrefix(sample, "<body")
}

func (h *Handler) findResourceForPage(originalURL string, pageID int64) (*models.Resource, error) {
	return h.findResourceForPageOnly(originalURL, pageID)
}

func (h *Handler) findResourceForPageOnly(originalURL string, pageID int64) (*models.Resource, error) {
	resource, err := h.db.GetLinkedResourceByURLAndPageID(originalURL, pageID)
	if err != nil || resource != nil {
		return resource, err
	}

	parsed, parseErr := url.Parse(originalURL)
	if parseErr == nil {
		encoded := parsed.String()
		if encoded != originalURL {
			resource, err = h.db.GetLinkedResourceByURLAndPageID(encoded, pageID)
			if err != nil || resource != nil {
				return resource, err
			}
		}
	}

	encodedURL := strings.ReplaceAll(originalURL, " ", "%20")
	if encodedURL != originalURL {
		resource, err = h.db.GetLinkedResourceByURLAndPageID(encodedURL, pageID)
		if err != nil || resource != nil {
			return resource, err
		}
	}

	resource, err = h.db.GetResourceByURLPrefix(originalURL, pageID)
	if err != nil || resource != nil {
		return resource, err
	}

	urlPath := originalURL
	if idx := strings.IndexByte(urlPath, '?'); idx != -1 {
		urlPath = urlPath[:idx]
	}

	resource, err = h.db.GetResourceByURLPath(urlPath, pageID)
	if err != nil || resource != nil {
		return resource, err
	}

	return nil, nil
}

func rewriteCSSForPage(parser interface {
	ExtractResources(string) []string
	RewriteCSS(string, map[string]string) string
}, cssContent, cssURL string, resolveFilePath func(string) (string, bool)) string {
	cssResources := parser.ExtractResources(cssContent)
	if len(cssResources) == 0 {
		return cssContent
	}

	urlMapping := make(map[string]string)
	for _, cssResURL := range cssResources {
		absoluteURL := resolveRelativeURL(cssURL, cssResURL)
		if absoluteURL == "" {
			continue
		}

		if filePath, ok := resolveFilePath(absoluteURL); ok {
			urlMapping[cssResURL] = filePath
			urlMapping[absoluteURL] = filePath
		}
	}

	if len(urlMapping) == 0 {
		return cssContent
	}

	return parser.RewriteCSS(cssContent, urlMapping)
}

func resolveRelativeURL(baseURL, relativeURL string) string {
	if strings.HasPrefix(relativeURL, "http://") || strings.HasPrefix(relativeURL, "https://") {
		parsed, err := url.Parse(relativeURL)
		if err != nil {
			return ""
		}
		return parsed.String()
	}

	base, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}

	rel, err := url.Parse(relativeURL)
	if err != nil {
		return ""
	}

	return base.ResolveReference(rel).String()
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
	case "html":
		return "text/html; charset=utf-8"
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
	case ".html", ".htm":
		return "text/html; charset=utf-8"
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

	serveFileStreaming(c, safePath)
}

// serveFileStreaming 流式提供文件，避免将整个文件加载到内存
func serveFileStreaming(c *gin.Context, filePath string) {
	f, err := os.Open(filePath)
	if err != nil {
		c.String(http.StatusNotFound, "Resource not found")
		return
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		c.String(http.StatusInternalServerError, "Failed to stat file")
		return
	}

	contentType := detectContentTypeByPath(filePath)
	c.Header("Content-Type", contentType)
	c.Header("Cache-Control", "public, max-age=31536000")
	http.ServeContent(c.Writer, c.Request, filepath.Base(filePath), stat.ModTime(), f)
}

// fixUnrewrittenSrcset 修复未重写的 srcset 协议相对 URL
// 早期归档的页面 srcset 属性未被重写，这里在渲染时补偿处理
// 策略：删除 <picture> 中的 <source> 元素，让浏览器回退到 <img> 标签（已被正确重写）
func fixUnrewrittenSrcset(html string) string {
	// 删除 <picture> 标签内的 <source> 元素
	// <source> 提供 avif/webp 等现代格式，但 srcset 未重写会导致加载失败
	// <img> 的 src 已被正确重写，删除 <source> 后浏览器会回退到 <img>
	html = sourceTagRe.ReplaceAllString(html, "")

	return html
}

func sanitizeArchivedHTML(html string) string {
	html = baseTagRe.ReplaceAllString(html, "")
	html = metaCSPRe.ReplaceAllString(html, "")
	html = scriptTagRe.ReplaceAllString(html, "")
	html = noscriptTagRe.ReplaceAllString(html, "")
	html = eventHandlerDQRe.ReplaceAllString(html, "")
	html = eventHandlerSQRe.ReplaceAllString(html, "")
	html = jsProtocolRe.ReplaceAllString(html, `href="#"`)
	html = lazyLoadRe.ReplaceAllString(html, "")
	html = hideVideoElements(html)
	html = removeUnsupportedEmbeddedContent(html)
	html = fixUnrewrittenSrcset(html)
	html = removeUnarchivedExternalCSSReferences(html)
	html = removeLoadingOverlays(html)
	html = fixNestedButtons(html)
	html = fixScrollAnimationOpacity(html)
	return html
}

// removeUnarchivedExternalCSSReferences 清理仍残留在 HTML 中的外部 CSS 引用。
// 这些引用通常对应下载失败的资源；保留它们只会让归档页继续请求原站。
func removeUnarchivedExternalCSSReferences(html string) string {
	html = cssURLRe.ReplaceAllStringFunc(html, func(match string) string {
		parts := cssURLRe.FindStringSubmatch(match)
		if len(parts) < 2 {
			return match
		}

		rawURL := strings.TrimSpace(htmlpkg.UnescapeString(parts[1]))
		rawURL = strings.Trim(rawURL, `"'`)
		if rawURL == "" {
			return match
		}

		if strings.HasPrefix(rawURL, "/archive/") || strings.HasPrefix(rawURL, "data:") {
			return match
		}

		if strings.HasPrefix(rawURL, "http://") || strings.HasPrefix(rawURL, "https://") || strings.HasPrefix(rawURL, "//") {
			return `url("")`
		}

		return match
	})

	html = externalImportRe.ReplaceAllString(html, "")
	return html
}

// removeUnsupportedEmbeddedContent 移除无法在静态归档页中可靠工作的嵌入式内容。
// ViewPage 已通过 CSP 禁用 frame/object，这里同步剥掉对应标签，避免旧归档页残留远程资源 URL。
func removeUnsupportedEmbeddedContent(html string) string {
	html = frameTagRe.ReplaceAllString(html, "")
	html = objectTagRe.ReplaceAllString(html, "")
	html = embedTagRe.ReplaceAllString(html, "")
	return html
}

// hideVideoElements 规范化归档页面中的 video 元素
// 1. 去掉 autoplay，避免归档页一打开就自动播放
// 2. 给可播放视频补上原生 controls，方便在无 JS 页面中手动播放
// 3. 隐藏既没有 src、也没有 <source> 的空 video，避免渲染黑块
func hideVideoElements(html string) string {
	// 匹配完整的 <video>...</video> 或自闭合 <video/>
	html = videoBlockRe.ReplaceAllStringFunc(html, func(match string) string {
		match = autoplayAttrRe.ReplaceAllString(match, "")

		// 有 src 属性 → 保留（不管是本地还是外部）
		if srcAttrRe.MatchString(match) {
			if !controlsAttrRe.MatchString(match) {
				return strings.Replace(match, "<video", `<video controls`, 1)
			}
			return match
		}
		// 内部有 <source> 子元素 → 保留
		if strings.Contains(strings.ToLower(match), "<source") {
			if !controlsAttrRe.MatchString(match) {
				return strings.Replace(match, "<video", `<video controls`, 1)
			}
			return match
		}
		// 无源的空 video → 隐藏
		return strings.Replace(match, "<video", `<video style="display:none!important"`, 1)
	})
	return html
}

// removeLoadingOverlays 移除 SPA 的全屏 loading 覆盖层
// 这些覆盖层在正常页面中由 JS 在加载完成后隐藏，但归档页面没有 JS，会永远遮挡内容
// 例如 X.com 的 #placeholder（黑色背景 + X logo）和 #ScriptLoadFailure（错误提示）
var (
	openDivRe           = regexp.MustCompile(`(?i)<div\b`)
	closeDivRe          = regexp.MustCompile(`(?i)</div>`)
	placeholderDivRe    = regexp.MustCompile(`(?i)<div\b[^>]*\bid="placeholder"[^>]*>`)
	scriptLoadFailDivRe = regexp.MustCompile(`(?i)<div\b[^>]*\bid="ScriptLoadFailure"[^>]*>`)
)

func removeLoadingOverlays(html string) string {
	for _, re := range []*regexp.Regexp{placeholderDivRe, scriptLoadFailDivRe} {
		loc := re.FindStringIndex(html)
		if loc == nil {
			continue
		}
		// Count nested divs to find the matching closing </div>
		start := loc[0]
		depth := 1
		pos := loc[1]
		for depth > 0 && pos < len(html) {
			rest := html[pos:]
			openLoc := openDivRe.FindStringIndex(rest)
			closeLoc := closeDivRe.FindStringIndex(rest)
			if closeLoc == nil {
				break
			}
			if openLoc != nil && openLoc[0] < closeLoc[0] {
				depth++
				pos += openLoc[1]
			} else {
				depth--
				pos += closeLoc[1]
			}
		}
		if depth == 0 {
			html = html[:start] + html[pos:]
		}
	}
	return html
}

// fixScrollAnimationOpacity 修复 JS 动画元素的 opacity: 0
// 许多网站使用 IntersectionObserver 或 JS 库实现动画，元素初始 opacity: 0，
// 动画触发后才变为 opacity: 1。归档页面没有 JS，这些元素永远不可见。
// 匹配两类元素：
// 1. 带有 data-animate* 属性且 style 中包含 opacity: 0 的元素
// 2. style 中同时包含 opacity: 0 和 stroke-dashoffset 的 SVG 元素（线条绘制动画）
var (
	scrollAnimRe = regexp.MustCompile(
		`(<[^>]*\bdata-animate(?:-\w+)?\b[^>]*\bstyle=")((?:[^"]*)opacity:\s*0[^"]*)(")`,
	)
	svgAnimRe = regexp.MustCompile(
		`(?i)(<(?:path|circle|line|polyline|polygon|rect|ellipse)[\s>][^>]*\bstyle=")((?:[^"]*)opacity:\s*0[^"]*)(")`,
	)
)

func fixScrollAnimationOpacity(html string) string {
	html = scrollAnimRe.ReplaceAllStringFunc(html, func(match string) string {
		return cleanAnimationStyle(scrollAnimRe, match)
	})
	html = svgAnimRe.ReplaceAllStringFunc(html, func(match string) string {
		return cleanAnimationStyle(svgAnimRe, match)
	})
	return html
}

func cleanAnimationStyle(re *regexp.Regexp, match string) string {
	sub := re.FindStringSubmatch(match)
	if len(sub) < 4 {
		return match
	}
	prefix := sub[1]  // <tag ... style="
	style := sub[2]   // style content
	closing := sub[3] // "

	// 移除动画相关的内联样式属性
	cleaned := animOpacityRe.ReplaceAllString(style, "")
	cleaned = animTransformRe.ReplaceAllString(cleaned, "")
	cleaned = animTranslateRe.ReplaceAllString(cleaned, "")
	cleaned = animRotateRe.ReplaceAllString(cleaned, "")
	cleaned = animScaleRe.ReplaceAllString(cleaned, "")
	cleaned = animStrokeDashoffsetRe.ReplaceAllString(cleaned, "")
	// 清理多余的分号和空格
	cleaned = multiSemiRe.ReplaceAllString(cleaned, ";")
	cleaned = strings.TrimLeft(cleaned, "; ")
	cleaned = strings.TrimRight(cleaned, "; ")

	return prefix + cleaned + closing
}

var (
	animOpacityRe          = regexp.MustCompile(`\bopacity:\s*0\s*;?`)
	animTransformRe        = regexp.MustCompile(`\btransform:\s*[^;]+;?`)
	animTranslateRe        = regexp.MustCompile(`\btranslate:\s*[^;]+;?`)
	animRotateRe           = regexp.MustCompile(`\brotate:\s*[^;]+;?`)
	animScaleRe            = regexp.MustCompile(`\bscale:\s*[^;]+;?`)
	animStrokeDashoffsetRe = regexp.MustCompile(`\bstroke-dashoffset:\s*[^;]+;?`)
	multiSemiRe            = regexp.MustCompile(`;\s*;+`)
)
