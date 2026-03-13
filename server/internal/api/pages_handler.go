package api

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"wayback/internal/storage"
)

// ListPages 列出所有归档页面（支持分页和时间过滤）
func (h *Handler) ListPages(c *gin.Context) {
	// 从查询参数获取分页信息，默认每页100条
	limit := 100
	offset := 0

	if limitStr := c.Query("limit"); limitStr != "" {
		fmt.Sscanf(limitStr, "%d", &limit)
		if limit <= 0 || limit > 1000 {
			limit = 100
		}
	}

	if offsetStr := c.Query("offset"); offsetStr != "" {
		fmt.Sscanf(offsetStr, "%d", &offset)
		if offset < 0 {
			offset = 0
		}
	}

	// 解析时间参数
	var from, to *time.Time
	if fromStr := c.Query("from"); fromStr != "" {
		if t, err := time.Parse("2006-01-02", fromStr); err == nil {
			from = &t
		}
	}
	if toStr := c.Query("to"); toStr != "" {
		if t, err := time.Parse("2006-01-02", toStr); err == nil {
			to = &t
		}
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
		"pages": pages,
		"total": total,
		"limit": limit,
		"offset": offset,
	})
}

// GetPage 获取单个页面详情
func (h *Handler) GetPage(c *gin.Context) {
	id := c.Param("id")
	page, err := h.db.GetPageByID(id)
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

	// 解析时间参数
	var from, to *time.Time
	if fromStr := c.Query("from"); fromStr != "" {
		if t, err := time.Parse("2006-01-02", fromStr); err == nil {
			from = &t
		}
	}
	if toStr := c.Query("to"); toStr != "" {
		if t, err := time.Parse("2006-01-02", toStr); err == nil {
			to = &t
		}
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
	id := c.Param("id")

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
	var pageID int64
	fmt.Sscanf(id, "%d", &pageID)
	if err := h.db.DeletePage(pageID); err != nil {
		log.Printf("Failed to delete page %s: %v", id, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	log.Printf("Deleted page: %s (%s)", id, page.URL)
	c.JSON(http.StatusOK, gin.H{"status": "success"})
}

// GetPageContent 返回页面正文的 Markdown 格式（精简版，方便 AI 读取）
func (h *Handler) GetPageContent(c *gin.Context) {
	id := c.Param("id")
	page, err := h.db.GetPageByID(id)
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
