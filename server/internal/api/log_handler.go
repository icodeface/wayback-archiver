package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// ListLogs returns available log files.
func (h *Handler) ListLogs(c *gin.Context) {
	files, err := h.logger.ListLogFiles()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"files": files})
}

// GetLog returns the content of a specific log file.
func (h *Handler) GetLog(c *gin.Context) {
	filename := c.Param("filename")
	tail := 500
	if t := c.Query("tail"); t != "" {
		if v, err := strconv.Atoi(t); err == nil && v > 0 {
			tail = v
		}
	}

	content, err := h.logger.ReadLogFile(filename, tail)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"content": content, "filename": filename})
}
