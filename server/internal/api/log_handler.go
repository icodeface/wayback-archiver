package api

import (
	"net/http"
	"strconv"
	"strings"

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
	tail := 2000
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

	// Server-side grep filtering
	if grep := c.Query("grep"); grep != "" {
		lines := strings.Split(content, "\n")
		var filtered []string
		lowerGrep := strings.ToLower(grep)
		for _, line := range lines {
			if strings.Contains(strings.ToLower(line), lowerGrep) {
				filtered = append(filtered, line)
			}
		}
		content = strings.Join(filtered, "\n")
	}

	c.JSON(http.StatusOK, gin.H{"content": content, "filename": filename})
}

// GetLatestLog returns the content of the most recent log file.
func (h *Handler) GetLatestLog(c *gin.Context) {
	files, err := h.logger.ListLogFiles()
	if err != nil || len(files) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "no log files found"})
		return
	}

	// files are sorted newest first
	latest := files[0].Name

	tail := 2000
	if t := c.Query("tail"); t != "" {
		if v, err := strconv.Atoi(t); err == nil && v > 0 {
			tail = v
		}
	}

	content, err := h.logger.ReadLogFile(latest, tail)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Server-side grep filtering
	if grep := c.Query("grep"); grep != "" {
		lines := strings.Split(content, "\n")
		var filtered []string
		lowerGrep := strings.ToLower(grep)
		for _, line := range lines {
			if strings.Contains(strings.ToLower(line), lowerGrep) {
				filtered = append(filtered, line)
			}
		}
		content = strings.Join(filtered, "\n")
	}

	c.JSON(http.StatusOK, gin.H{"content": content, "filename": latest})
}
