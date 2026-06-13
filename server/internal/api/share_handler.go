package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"wayback/internal/models"
	"wayback/internal/storage"
)

const shareTokenBytes = 32

func generateShareToken() (string, error) {
	b := make([]byte, shareTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "wbs_" + base64.RawURLEncoding.EncodeToString(b), nil
}

func hashShareToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func sharePath(token string) string {
	return "/share/" + url.PathEscape(token)
}

func shareMarkdownPath(token string) string {
	return sharePath(token) + "/md"
}

func shareResponseFromModel(share *models.PageShare, token string) models.ShareResponse {
	resp := models.ShareResponse{
		Status:     "success",
		ID:         share.ID,
		Token:      token,
		PageID:     share.PageID,
		URL:        share.URL,
		Title:      share.Title,
		CapturedAt: share.CapturedAt,
		CreatedAt:  share.CreatedAt,
		ExpiresAt:  share.ExpiresAt,
	}
	if token != "" {
		resp.SnapshotURL = sharePath(token)
		if share.AllowMarkdown {
			resp.MarkdownURL = shareMarkdownPath(token)
		}
	}
	return resp
}

func (h *Handler) CreatePageShare(c *gin.Context) {
	pageID, ok := parsePageIDParam(c)
	if !ok {
		return
	}

	var req models.CreateShareRequest
	if c.Request.Body != nil && c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
			return
		}
	}
	if req.ExpiresAt != nil && !req.ExpiresAt.After(time.Now()) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "expires_at must be in the future"})
		return
	}
	allowMarkdown := true
	if req.AllowMarkdown != nil {
		allowMarkdown = *req.AllowMarkdown
	}

	page, err := h.db.GetPageByID(strconv.FormatInt(pageID, 10))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if page == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "page not found"})
		return
	}
	if page.SnapshotState != "" && page.SnapshotState != models.SnapshotStateReady {
		c.JSON(http.StatusConflict, gin.H{"error": "snapshot is not ready"})
		return
	}

	resources, err := h.db.GetResourcesByPageID(pageID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	resourceIDs := make([]int64, 0, len(resources))
	for _, resource := range resources {
		resourceIDs = append(resourceIDs, resource.ID)
	}

	var token string
	var share *models.PageShare
	for attempt := 0; attempt < 3; attempt++ {
		token, err = generateShareToken()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate share token"})
			return
		}
		share, err = h.db.CreatePageShare(hashShareToken(token), page, resourceIDs, req.ExpiresAt, allowMarkdown)
		if err == nil {
			c.JSON(http.StatusCreated, shareResponseFromModel(share, token))
			return
		}
		if !strings.Contains(strings.ToLower(err.Error()), "unique") {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create unique share token"})
}

func (h *Handler) ListPageShares(c *gin.Context) {
	pageID, ok := parsePageIDParam(c)
	if !ok {
		return
	}

	shares, err := h.db.ListPageShares(pageID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	responses := make([]models.ShareResponse, 0, len(shares))
	for i := range shares {
		responses = append(responses, shareResponseFromModel(&shares[i], ""))
	}
	c.JSON(http.StatusOK, gin.H{"shares": responses})
}

func (h *Handler) RevokePageShare(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid share id"})
		return
	}

	if err := h.db.RevokePageShare(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "success"})
}

func (h *Handler) getActiveShare(c *gin.Context) (*models.PageShare, string, bool) {
	token := c.Param("token")
	if token == "" {
		c.String(http.StatusNotFound, "Share not found")
		return nil, "", false
	}

	share, err := h.db.GetActivePageShareByTokenHash(hashShareToken(token))
	if err != nil {
		c.String(http.StatusInternalServerError, "Database error")
		return nil, "", false
	}
	if share == nil {
		c.String(http.StatusNotFound, "Share not found")
		return nil, "", false
	}

	c.Header("X-Robots-Tag", "noindex, nofollow")
	return share, token, true
}

func (h *Handler) ViewSharedPage(c *gin.Context) {
	share, token, ok := h.getActiveShare(c)
	if !ok {
		return
	}

	htmlPath := filepath.Join(h.dataDir, share.HTMLPath)
	htmlContent, err := os.ReadFile(htmlPath)
	if err != nil {
		c.String(http.StatusNotFound, "Snapshot file not found")
		return
	}

	modifiedHTML := sanitizeArchivedHTML(string(htmlContent))
	modifiedHTML = rewriteHTMLForShare(modifiedHTML, token)

	nonce := generateNonce()
	modifiedHTML = injectPublicShareHeader(modifiedHTML, share, token, nonce)

	c.Header("X-Frame-Options", "SAMEORIGIN")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("Content-Security-Policy", fmt.Sprintf("default-src 'self'; script-src 'nonce-%s'; img-src * data: blob:; style-src 'self' 'unsafe-inline'; font-src * data:; connect-src 'none'; frame-src 'self'; object-src 'none';", nonce))
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(modifiedHTML))
}

func (h *Handler) ViewSharedPageMarkdown(c *gin.Context) {
	share, _, ok := h.getActiveShare(c)
	if !ok {
		return
	}
	if !share.AllowMarkdown {
		c.String(http.StatusNotFound, "Share not found")
		return
	}

	htmlPath := filepath.Join(h.dataDir, share.HTMLPath)
	htmlContent, err := os.ReadFile(htmlPath)
	if err != nil {
		c.String(http.StatusNotFound, "Snapshot file not found")
		return
	}

	markdown := storage.ExtractMarkdown(string(htmlContent))
	c.Header("Content-Type", "text/markdown; charset=utf-8")
	c.String(http.StatusOK, markdown)
}

func (h *Handler) ProxySharedResource(c *gin.Context) {
	share, _, ok := h.getActiveShare(c)
	if !ok {
		return
	}

	timestamp := c.Param("timestamp")
	rawPath := c.Request.URL.RawPath
	if rawPath == "" {
		rawPath = c.Request.URL.Path
	}

	prefix := fmt.Sprintf("/share/%s/archive/%s/", c.Param("token"), timestamp)
	originalURL := strings.TrimPrefix(rawPath, prefix)
	if c.Request.URL.RawQuery != "" {
		originalURL = originalURL + "?" + c.Request.URL.RawQuery
	}

	if strings.HasPrefix(originalURL, "resources/") {
		resourcePath := strings.TrimPrefix(originalURL, "resources/")
		h.serveSharedLocalResource(c, share, resourcePath)
		return
	}

	resource, err := h.findResourceForShare(originalURL, share.TokenHash)
	if err != nil {
		log.Printf("[Share] Database error: %v", err)
		c.String(http.StatusInternalServerError, "Database error")
		return
	}
	if resource == nil {
		log.Printf("[Share] Resource not found: %s", originalURL)
		c.String(http.StatusNotFound, "Resource not found")
		return
	}

	if resource.ResourceType == "css" {
		h.serveRewrittenCSSWithResolver(c, resource, "/share/"+url.PathEscape(c.Param("token"))+"/", func(resourceURL string) (string, bool) {
			cssResource, findErr := h.findResourceForShare(resourceURL, share.TokenHash)
			if findErr != nil {
				log.Printf("[Share] Failed to resolve CSS sub-resource %s: %v", resourceURL, findErr)
				return "", false
			}
			if cssResource == nil {
				return "", false
			}
			return cssResource.FilePath, true
		})
		return
	}
	if h.shouldServeArchivedHTML(c, resource) {
		h.serveSharedArchivedHTMLResource(c, resource, c.Param("token"))
		return
	}

	h.serveResourceFile(c, resource)
}

func (h *Handler) ServeSharedLocalResource(c *gin.Context) {
	share, _, ok := h.getActiveShare(c)
	if !ok {
		return
	}

	resourcePath := strings.TrimPrefix(c.Param("filepath"), "/")
	h.serveSharedLocalResource(c, share, resourcePath)
}

func (h *Handler) serveSharedLocalResource(c *gin.Context, share *models.PageShare, resourcePath string) {
	resourcePath = strings.TrimPrefix(resourcePath, "/")
	resource, err := h.db.GetShareResourceByFilePath(share.TokenHash, "resources/"+resourcePath)
	if err != nil {
		c.String(http.StatusInternalServerError, "Database error")
		return
	}
	if resource == nil {
		c.String(http.StatusNotFound, "Resource not found")
		return
	}

	resourcesDir := filepath.Join(h.dataDir, "resources")
	safePath, err := validateResourcePath(resourcesDir, resourcePath)
	if err != nil {
		log.Printf("[Share] Path validation failed for %s: %v", resourcePath, err)
		c.String(http.StatusForbidden, "Invalid resource path")
		return
	}
	serveFileStreaming(c, safePath)
}

func (h *Handler) serveResourceFile(c *gin.Context, resource *models.Resource) {
	filePath := filepath.Join(h.dataDir, resource.FilePath)
	c.Header("Content-Type", detectContentType(resource))
	c.Header("Cache-Control", "public, max-age=31536000")

	f, err := os.Open(filePath)
	if err != nil {
		log.Printf("[Share] Failed to read file %s: %v", filePath, err)
		c.String(http.StatusInternalServerError, "Failed to read file")
		return
	}
	defer f.Close()
	stat, _ := f.Stat()
	http.ServeContent(c.Writer, c.Request, filepath.Base(filePath), stat.ModTime(), f)
}

func (h *Handler) serveSharedArchivedHTMLResource(c *gin.Context, resource *models.Resource, token string) {
	filePath := filepath.Join(h.dataDir, resource.FilePath)
	htmlContent, err := os.ReadFile(filePath)
	if err != nil {
		log.Printf("[Share] Failed to read HTML file %s: %v", filePath, err)
		c.String(http.StatusInternalServerError, "Failed to read file")
		return
	}

	sanitized := sanitizeArchivedHTML(string(htmlContent))
	sanitized = rewriteHTMLForShare(sanitized, token)
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Header("Cache-Control", "public, max-age=31536000")
	c.Header("Content-Security-Policy", "default-src 'self'; script-src 'none'; img-src * data: blob:; style-src 'self' 'unsafe-inline'; font-src * data:; connect-src 'none'; frame-src 'self'; object-src 'none';")
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(sanitized))
}

func (h *Handler) findResourceForShare(originalURL, tokenHash string) (*models.Resource, error) {
	resource, err := h.db.GetShareResourceByURL(tokenHash, originalURL)
	if err != nil || resource != nil {
		return resource, err
	}

	parsed, parseErr := url.Parse(originalURL)
	if parseErr == nil {
		encoded := parsed.String()
		if encoded != originalURL {
			resource, err = h.db.GetShareResourceByURL(tokenHash, encoded)
			if err != nil || resource != nil {
				return resource, err
			}
		}
	}

	encodedURL := strings.ReplaceAll(originalURL, " ", "%20")
	if encodedURL != originalURL {
		resource, err = h.db.GetShareResourceByURL(tokenHash, encodedURL)
		if err != nil || resource != nil {
			return resource, err
		}
	}

	resource, err = h.db.GetShareResourceByURLPrefix(tokenHash, originalURL)
	if err != nil || resource != nil {
		return resource, err
	}

	urlPath := originalURL
	if idx := strings.IndexByte(urlPath, '?'); idx != -1 {
		urlPath = urlPath[:idx]
	}

	return h.db.GetShareResourceByURLPath(tokenHash, urlPath)
}

func rewriteHTMLForShare(html, token string) string {
	shareArchivePrefix := "/share/" + url.PathEscape(token) + "/archive/"
	html = shareArchivePathRe.ReplaceAllString(html, shareArchivePrefix+"$1/")
	html = strings.ReplaceAll(html, "/archive/resources/", "/share/"+url.PathEscape(token)+"/resources/")
	return html
}

func injectPublicShareHeader(html string, share *models.PageShare, token, nonce string) string {
	markdownLink := ""
	if share.AllowMarkdown {
		markdownLink = fmt.Sprintf(`<a href="%s" style="color:white;text-decoration:none;padding:4px 10px;border:1px solid rgba(255,255,255,0.3);border-radius:4px;font-size:12px;background:rgba(255,255,255,0.1);white-space:nowrap;">Markdown</a>`, escapeHTML(shareMarkdownPath(token)))
	}

	archiveHeader := fmt.Sprintf(`
<div id="wayback-archive-header" style="
	position: fixed;
	top: 0;
	left: 0;
	right: 0;
	height: 48px;
	background: linear-gradient(135deg, #334155 0%%, #0f766e 100%%);
	color: white;
	padding: 0 20px;
	box-shadow: 0 2px 8px rgba(0,0,0,0.15);
	z-index: 999999;
	font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
	font-size: 13px;
	display: flex;
	align-items: center;
	justify-content: space-between;
	gap: 16px;
	overflow: hidden;
">
	<div style="display:flex;align-items:center;gap:12px;min-width:0;flex:1;overflow:hidden;">
		<span style="background:rgba(255,255,255,0.2);padding:3px 10px;border-radius:4px;font-size:11px;font-weight:600;letter-spacing:0.5px;white-space:nowrap;">PUBLIC SNAPSHOT</span>
		<a href="%s" style="color:white;text-decoration:none;font-family:monospace;font-size:12px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;min-width:0;opacity:0.95;" title="%s">%s</a>
		<span style="font-size:11px;opacity:0.7;white-space:nowrap;">%s</span>
	</div>
	<div style="display:flex;align-items:center;gap:8px;flex-shrink:0;">%s</div>
</div>
<style>
	:root { --wayback-header-height: 48px; }
	body { margin-top: var(--wayback-header-height) !important; padding-top: 0 !important; }
	[style*="position: fixed"][style*="top: 0"]:not(#wayback-archive-header),
	[style*="position:fixed"][style*="top: 0"]:not(#wayback-archive-header),
	[style*="position: fixed"][style*="top:0"]:not(#wayback-archive-header),
	[style*="position:fixed"][style*="top:0"]:not(#wayback-archive-header),
	[style*="position: sticky"][style*="top: 0"]:not(#wayback-archive-header),
	[style*="position:sticky"][style*="top: 0"]:not(#wayback-archive-header),
	[style*="position: sticky"][style*="top:0"]:not(#wayback-archive-header),
	[style*="position:sticky"][style*="top:0"]:not(#wayback-archive-header) {
		top: var(--wayback-header-height) !important;
	}
	html, body, #app, #root, #__next, #__nuxt {
		height: auto !important;
		min-height: 100%% !important;
		overflow: visible !important;
	}
	* {
		user-select: text !important;
		-webkit-user-select: text !important;
	}
</style>
<script nonce="%s">
(function() {
	'use strict';
	document.querySelectorAll('.wayback-local-time').forEach(function(el) {
		const isoTime = el.getAttribute('datetime');
		if (!isoTime) return;
		const date = new Date(isoTime);
		if (Number.isNaN(date.getTime())) return;
		el.textContent = date.toLocaleString('zh-CN', {
			year: 'numeric',
			month: 'short',
			day: 'numeric',
			hour: '2-digit',
			minute: '2-digit',
			second: '2-digit'
		});
	});
})();
</script>
	`, escapeHTML(share.URL), escapeHTML(share.URL), escapeHTML(share.URL), archiveTimeElement(share.CapturedAt, "full"), markdownLink, nonce)

	if bodyTagRe.MatchString(html) {
		return bodyTagRe.ReplaceAllString(html, "$1"+archiveHeader)
	}
	return archiveHeader + html
}
