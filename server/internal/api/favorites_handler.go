package api

import (
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"wayback/internal/models"
)

// AddFavorite 添加收藏
func (h *Handler) AddFavorite(c *gin.Context) {
	pageID, ok := parsePageIDParam(c)
	if !ok {
		return
	}

	// 先确认页面存在，否则外键约束错误会被当成 500 返回
	page, err := h.db.GetPageByID(strconv.FormatInt(pageID, 10))
	if err != nil {
		log.Printf("Failed to load page %d: %v", pageID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load page"})
		return
	}
	if page == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "page not found"})
		return
	}

	if err := h.db.AddFavorite(pageID); err != nil {
		log.Printf("Failed to add favorite for page %d: %v", pageID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to add favorite"})
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to remove favorite"})
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check favorite status"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"is_favorite": isFavorite})
}

// attachFavoriteStates adds favorite state to a page-list response with one
// database query. The field remains optional for detail and timeline responses.
func (h *Handler) attachFavoriteStates(pages []models.Page) error {
	if len(pages) == 0 {
		return nil
	}

	pageIDs := make([]int64, len(pages))
	for i := range pages {
		pageIDs[i] = pages[i].ID
	}
	states, err := h.db.IsFavoriteBatch(pageIDs)
	if err != nil {
		return err
	}
	for i := range pages {
		isFavorite := states[pages[i].ID]
		pages[i].IsFavorite = &isFavorite
	}
	return nil
}

func markFavoritePages(pages []models.Page) {
	for i := range pages {
		isFavorite := true
		pages[i].IsFavorite = &isFavorite
	}
}

// ListFavorites 列出收藏的页面
func (h *Handler) ListFavorites(c *gin.Context) {
	limit, offset, ok := parsePaginationParams(c)
	if !ok {
		return
	}

	pages, err := h.db.ListFavorites(limit, offset)
	if err != nil {
		log.Printf("Failed to list favorites: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list favorites"})
		return
	}
	markFavoritePages(pages)

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

	// 限制搜索关键字最大长度
	if len(keyword) > 200 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "query too long (max 200 characters)"})
		return
	}

	limit, offset, ok := parsePaginationParams(c)
	if !ok {
		return
	}

	pages, err := h.db.SearchFavorites(keyword, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "search failed"})
		return
	}
	markFavoritePages(pages)

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
