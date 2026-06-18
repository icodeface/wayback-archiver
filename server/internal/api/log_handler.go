package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

func parseTailQuery(c *gin.Context) (int, bool) {
	tail := 2000
	if t := c.Query("tail"); t != "" {
		v, err := strconv.Atoi(t)
		if err != nil || v <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid tail"})
			return 0, false
		}
		tail = v
	}
	return tail, true
}

func parseOptionalInt64Query(c *gin.Context, name string) (int64, bool, bool) {
	raw := c.Query(name)
	if raw == "" {
		return 0, false, true
	}

	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid %s", name)})
		return 0, false, false
	}
	return v, true, true
}

func parseLogRangeLimitQuery(c *gin.Context) (int64, bool) {
	raw := c.Query("limit")
	if raw == "" {
		return 0, true
	}

	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid limit"})
		return 0, false
	}
	return v, true
}

func isLogRangeRequest(c *gin.Context) bool {
	return c.Query("before") != "" || c.Query("after") != "" || c.Query("limit") != ""
}

func logReadStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}

	message := err.Error()
	switch {
	case errors.Is(err, strconv.ErrSyntax), strings.Contains(message, "invalid filename"), strings.Contains(message, "invalid log filename"):
		return http.StatusBadRequest
	case strings.Contains(message, "symlink not allowed"), strings.Contains(message, "log file too large"):
		return http.StatusInternalServerError
	default:
		return http.StatusNotFound
	}
}

func (h *Handler) getLogRange(c *gin.Context, filename string) {
	before, hasBefore, ok := parseOptionalInt64Query(c, "before")
	if !ok {
		return
	}
	after, hasAfter, ok := parseOptionalInt64Query(c, "after")
	if !ok {
		return
	}
	if hasBefore && hasAfter {
		c.JSON(http.StatusBadRequest, gin.H{"error": "before and after cannot be used together"})
		return
	}
	limit, ok := parseLogRangeLimitQuery(c)
	if !ok {
		return
	}

	var beforePtr, afterPtr *int64
	if hasBefore {
		beforePtr = &before
	}
	if hasAfter {
		afterPtr = &after
	}

	result, err := h.logger.ReadLogRange(filename, beforePtr, afterPtr, limit)
	if err != nil {
		c.JSON(logReadStatus(err), gin.H{"error": err.Error()})
		return
	}

	content := result.Content
	if grep := c.Query("grep"); grep != "" {
		content = filterLogContent(content, grep)
	}

	c.JSON(http.StatusOK, gin.H{
		"content":         content,
		"filename":        filename,
		"start_offset":    result.StartOffset,
		"end_offset":      result.EndOffset,
		"file_size":       result.FileSize,
		"has_more_before": result.HasMoreBefore,
		"has_more_after":  result.HasMoreAfter,
	})
}

func filterLogContent(content, grep string) string {
	lines := strings.Split(content, "\n")
	var filtered []string
	lowerGrep := strings.ToLower(grep)
	for _, line := range lines {
		if strings.Contains(strings.ToLower(line), lowerGrep) {
			filtered = append(filtered, line)
		}
	}
	return strings.Join(filtered, "\n")
}

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
	if isLogRangeRequest(c) {
		h.getLogRange(c, filename)
		return
	}

	tail, ok := parseTailQuery(c)
	if !ok {
		return
	}

	content, err := h.logger.ReadLogFile(filename, tail)
	if err != nil {
		c.JSON(logReadStatus(err), gin.H{"error": err.Error()})
		return
	}

	// Server-side grep filtering
	if grep := c.Query("grep"); grep != "" {
		content = filterLogContent(content, grep)
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

	if isLogRangeRequest(c) {
		h.getLogRange(c, latest)
		return
	}

	tail, ok := parseTailQuery(c)
	if !ok {
		return
	}

	content, err := h.logger.ReadLogFile(latest, tail)
	if err != nil {
		c.JSON(logReadStatus(err), gin.H{"error": err.Error()})
		return
	}

	// Server-side grep filtering
	if grep := c.Query("grep"); grep != "" {
		content = filterLogContent(content, grep)
	}

	c.JSON(http.StatusOK, gin.H{"content": content, "filename": latest})
}
