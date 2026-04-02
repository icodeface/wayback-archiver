package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestServeFileStreaming_SmallFile(t *testing.T) {
	gin.SetMode(gin.TestMode)

	dir := t.TempDir()
	resourcesDir := filepath.Join(dir, "resources", "ab", "cd")
	os.MkdirAll(resourcesDir, 0755)

	filePath := filepath.Join(resourcesDir, "test.css")
	content := "body { color: red; }"
	os.WriteFile(filePath, []byte(content), 0644)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/test.css", nil)

	serveFileStreaming(c, filePath)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if w.Body.String() != content {
		t.Errorf("body = %q, want %q", w.Body.String(), content)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/css") {
		t.Errorf("Content-Type = %q, want text/css", ct)
	}
	if w.Header().Get("Cache-Control") != "public, max-age=31536000" {
		t.Errorf("Cache-Control = %q, want public, max-age=31536000", w.Header().Get("Cache-Control"))
	}
}

func TestServeFileStreaming_LargeFile(t *testing.T) {
	gin.SetMode(gin.TestMode)

	dir := t.TempDir()
	filePath := filepath.Join(dir, "large.img")

	// 1MB 文件
	data := make([]byte, 1024*1024)
	for i := range data {
		data[i] = byte(i % 256)
	}
	os.WriteFile(filePath, data, 0644)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/large.img", nil)

	serveFileStreaming(c, filePath)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if w.Body.Len() != len(data) {
		t.Errorf("body size = %d, want %d", w.Body.Len(), len(data))
	}
}

func TestServeFileStreaming_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/missing.css", nil)

	serveFileStreaming(c, "/nonexistent/path/file.css")

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestServeFileStreaming_ContentTypes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		filename    string
		content     string
		wantType    string
	}{
		{"style.css", "body{}", "text/css"},
		{"script.js", "var x=1;", "javascript"},
		{"image.img", "\x89PNG", "application/octet-stream"},
		{"font.woff2", "\x00\x01", "font/woff2"},
		{"font.ttf", "\x00\x01", "font/ttf"},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			dir := t.TempDir()
			filePath := filepath.Join(dir, tt.filename)
			os.WriteFile(filePath, []byte(tt.content), 0644)

			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", "/"+tt.filename, nil)

			serveFileStreaming(c, filePath)

			ct := w.Header().Get("Content-Type")
			if !strings.Contains(ct, tt.wantType) {
				t.Errorf("Content-Type = %q, want containing %q", ct, tt.wantType)
			}
		})
	}
}

func TestServeFileStreaming_HeadRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	dir := t.TempDir()
	filePath := filepath.Join(dir, "test.css")
	content := "body { color: blue; }"
	os.WriteFile(filePath, []byte(content), 0644)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("HEAD", "/test.css", nil)

	serveFileStreaming(c, filePath)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	// HEAD 请求不应有 body
	if w.Body.Len() != 0 {
		t.Errorf("HEAD response body should be empty, got %d bytes", w.Body.Len())
	}
}
