package api

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"wayback/internal/storage"
)

func parsePageIDParam(c *gin.Context) (int64, bool) {
	idStr := c.Param("id")
	pageID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || pageID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid page id"})
		return 0, false
	}
	return pageID, true
}

func parsePaginationParams(c *gin.Context) (int, int, bool) {
	limit := 100
	offset := 0

	if limitStr := c.Query("limit"); limitStr != "" {
		parsed, err := strconv.Atoi(limitStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid limit"})
			return 0, 0, false
		}
		if parsed <= 0 || parsed > 1000 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "limit must be between 1 and 1000"})
			return 0, 0, false
		}
		limit = parsed
	}

	if offsetStr := c.Query("offset"); offsetStr != "" {
		parsed, err := strconv.Atoi(offsetStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid offset"})
			return 0, 0, false
		}
		if parsed < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "offset must be non-negative"})
			return 0, 0, false
		}
		offset = parsed
	}

	return limit, offset, true
}

func parseDateFilters(c *gin.Context) (*time.Time, *time.Time, bool) {
	var from, to *time.Time
	if fromStr := c.Query("from"); fromStr != "" {
		t, err := time.Parse("2006-01-02", fromStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid from date, expected YYYY-MM-DD"})
			return nil, nil, false
		}
		from = &t
	}
	if toStr := c.Query("to"); toStr != "" {
		t, err := time.Parse("2006-01-02", toStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid to date, expected YYYY-MM-DD"})
			return nil, nil, false
		}
		to = &t
	}
	return from, to, true
}

// ListPages 列出所有归档页面（支持分页和时间过滤）
func (h *Handler) ListPages(c *gin.Context) {
	limit, offset, ok := parsePaginationParams(c)
	if !ok {
		return
	}

	from, to, ok := parseDateFilters(c)
	if !ok {
		return
	}

	domain := c.Query("domain")

	pages, err := h.db.ListPages(limit, offset, from, to, domain)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 获取总数
	total, err := h.db.GetTotalPagesCount(from, to, domain)
	if err != nil {
		log.Printf("Failed to get total count: %v", err)
		total = len(pages)
	}

	c.JSON(http.StatusOK, gin.H{
		"pages":  pages,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// GetPage 获取单个页面详情
func (h *Handler) GetPage(c *gin.Context) {
	pageID, ok := parsePageIDParam(c)
	if !ok {
		return
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
	c.JSON(http.StatusOK, page)
}

// SearchPages 搜索页面（支持时间过滤）
func (h *Handler) SearchPages(c *gin.Context) {
	keyword := c.Query("q")
	if keyword == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing query parameter"})
		return
	}

	from, to, ok := parseDateFilters(c)
	if !ok {
		return
	}

	domain := c.Query("domain")

	pages, err := h.db.SearchPages(keyword, from, to, domain)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, pages)
}

// GetPageTimeline 获取同一 URL 的所有快照
func (h *Handler) GetPageTimeline(c *gin.Context) {
	pageURL := c.Query("url")
	if pageURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing url parameter"})
		return
	}

	pages, err := h.db.GetPagesByURL(pageURL)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"url":       pageURL,
		"snapshots": pages,
		"total":     len(pages),
	})
}

// DeletePage 删除页面
func (h *Handler) DeletePage(c *gin.Context) {
	pageID, ok := parsePageIDParam(c)
	if !ok {
		return
	}
	id := strconv.FormatInt(pageID, 10)

	// 先检查页面是否存在
	page, err := h.db.GetPageByID(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if page == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "page not found"})
		return
	}

	// 删除页面记录
	if err := h.db.DeletePage(pageID); err != nil {
		log.Printf("Failed to delete page %s: %v", id, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 将 HTML 文件加入删除队列（7 天后自动删除）
	if page.HTMLPath != "" {
		if err := h.dedup.AddHTMLToDeletionQueue(page.HTMLPath, pageID); err != nil {
			log.Printf("Failed to add HTML to deletion queue for page %s: %v", id, err)
		}
	}

	log.Printf("Deleted page: %s (%s)", id, page.URL)
	c.JSON(http.StatusOK, gin.H{"status": "success"})
}

// GetPageContent 返回页面正文的 Markdown 格式（精简版，方便 AI 读取）
func (h *Handler) GetPageContent(c *gin.Context) {
	pageID, ok := parsePageIDParam(c)
	if !ok {
		return
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

	htmlPath := filepath.Join(h.dataDir, page.HTMLPath)
	htmlContent, err := os.ReadFile(htmlPath)
	if err != nil {
		c.String(http.StatusInternalServerError, "Failed to read HTML file")
		return
	}

	markdown := storage.ExtractMarkdown(string(htmlContent))

	c.Header("Content-Type", "text/markdown; charset=utf-8")
	c.String(http.StatusOK, markdown)
}
