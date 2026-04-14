package api

import (
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"wayback/internal/models"
)

// ArchivePage 处理页面归档请求
func (h *Handler) ArchivePage(c *gin.Context) {
	var req models.CaptureRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	log.Printf("Received archive request: %s (frames: %d)", req.URL, len(req.Frames))

	// 处理捕获
	pageID, action, err := h.dedup.ProcessCapture(&req)
	if err != nil {
		log.Printf("Failed to process capture: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, models.ArchiveResponse{
		Status: "success",
		PageID: pageID,
		Action: action,
	})
}

// UpdatePage 处理页面更新请求
func (h *Handler) UpdatePage(c *gin.Context) {
	idStr := c.Param("id")
	pageID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid page id"})
		return
	}

	var req models.CaptureRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	log.Printf("Received update request for page %d: %s", pageID, req.URL)

	action, err := h.dedup.UpdateCapture(pageID, &req)
	if err != nil {
		log.Printf("Failed to update capture: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, models.ArchiveResponse{
		Status: "success",
		PageID: pageID,
		Action: action,
	})
}
