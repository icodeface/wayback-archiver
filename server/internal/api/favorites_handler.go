package api

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

// AddFavorite 添加收藏
func (h *Handler) AddFavorite(c *gin.Context) {
	pageID, ok := parsePageIDParam(c)
	if !ok {
		return
	}

	if err := h.db.AddFavorite(pageID); err != nil {
		log.Printf("Failed to add favorite for page %d: %v", pageID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "success"})
}

// RemoveFavorite 取消收藏
func (h *Handler) RemoveFavorite(c *gin.Context) {
	pageID, ok := parsePageIDParam(c)
	if !ok {
		return
	}

	if err := h.db.RemoveFavorite(pageID); err != nil {
		log.Printf("Failed to remove favorite for page %d: %v", pageID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "success"})
}

// IsFavorite 检查是否已收藏
func (h *Handler) IsFavorite(c *gin.Context) {
	pageID, ok := parsePageIDParam(c)
	if !ok {
		return
	}

	isFavorite, err := h.db.IsFavorite(pageID)
	if err != nil {
		log.Printf("Failed to check favorite status for page %d: %v", pageID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"is_favorite": isFavorite})
}

// ListFavorites 列出收藏的页面
func (h *Handler) ListFavorites(c *gin.Context) {
	limit, offset, ok := parsePaginationParams(c)
	if !ok {
		return
	}

	pages, err := h.db.ListFavorites(limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	total, err := h.db.GetFavoritesCount()
	if err != nil {
		log.Printf("Failed to get favorites count: %v", err)
		total = len(pages)
	}

	c.JSON(http.StatusOK, gin.H{
		"pages":  pages,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// SearchFavorites 在收藏中搜索
func (h *Handler) SearchFavorites(c *gin.Context) {
	keyword := c.Query("q")
	if keyword == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing query parameter"})
		return
	}

	limit, offset, ok := parsePaginationParams(c)
	if !ok {
		return
	}

	pages, err := h.db.SearchFavorites(keyword, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	total, err := h.db.GetSearchFavoritesCount(keyword)
	if err != nil {
		log.Printf("Failed to get search favorites count: %v", err)
		total = len(pages)
	}

	c.JSON(http.StatusOK, gin.H{
		"pages":  pages,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}
