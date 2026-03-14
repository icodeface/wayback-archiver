package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestPathTraversalAttacks 测试路径穿越攻击防护
func TestPathTraversalAttacks(t *testing.T) {
	// 创建临时测试目录
	tmpDir := t.TempDir()
	resourcesDir := filepath.Join(tmpDir, "resources")
	if err := os.MkdirAll(resourcesDir, 0755); err != nil {
		t.Fatalf("Failed to create resources dir: %v", err)
	}

	// 创建合法的测试文件
	validFile := filepath.Join(resourcesDir, "test.css")
	if err := os.WriteFile(validFile, []byte("body { color: red; }"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// 创建敏感文件（模拟系统文件）
	sensitiveFile := filepath.Join(tmpDir, "secret.txt")
	if err := os.WriteFile(sensitiveFile, []byte("SECRET_DATA"), 0644); err != nil {
		t.Fatalf("Failed to create sensitive file: %v", err)
	}

	// 初始化 handler（不需要真实数据库连接，因为测试只验证路径安全性）
	handler := &Handler{
		db:      nil, // 路径验证测试不需要数据库
		dataDir: tmpDir,
	}

	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		endpoint       string
		path           string
		expectedStatus int
		shouldContain  string
	}{
		{
			name:           "ServeLocalResource - valid path",
			endpoint:       "/archive/resources/",
			path:           "test.css",
			expectedStatus: http.StatusOK,
			shouldContain:  "body { color: red; }",
		},
		{
			name:           "ServeLocalResource - path traversal with ../",
			endpoint:       "/archive/resources/",
			path:           "../secret.txt",
			expectedStatus: http.StatusForbidden,
			shouldContain:  "Invalid resource path",
		},
		{
			name:           "ServeLocalResource - path traversal with ../../",
			endpoint:       "/archive/resources/",
			path:           "../../secret.txt",
			expectedStatus: http.StatusForbidden,
			shouldContain:  "Invalid resource path",
		},
		{
			name:           "ServeLocalResource - path traversal with absolute path",
			endpoint:       "/archive/resources/",
			path:           "/etc/passwd",
			expectedStatus: http.StatusForbidden,
			shouldContain:  "Invalid resource path",
		},
		{
			name:           "ServeLocalResource - encoded path traversal",
			endpoint:       "/archive/resources/",
			path:           "..%2F..%2Fsecret.txt",
			expectedStatus: http.StatusForbidden,
			shouldContain:  "Invalid resource path",
		},
		{
			name:           "ServeLocalResource - double encoded path traversal",
			endpoint:       "/archive/resources/",
			path:           "..%252F..%252Fsecret.txt",
			expectedStatus: http.StatusForbidden,
			shouldContain:  "Invalid resource path",
		},
		{
			name:           "ProxyResource - valid local resource",
			endpoint:       "/archive/123/20240309150405mp_/",
			path:           "resources/test.css",
			expectedStatus: http.StatusOK,
			shouldContain:  "body { color: red; }",
		},
		{
			name:           "ProxyResource - path traversal attempt",
			endpoint:       "/archive/123/20240309150405mp_/",
			path:           "resources/../secret.txt",
			expectedStatus: http.StatusForbidden,
			shouldContain:  "Invalid resource path",
		},
		{
			name:           "ProxyResource - path traversal with multiple ../",
			endpoint:       "/archive/123/20240309150405mp_/",
			path:           "resources/../../secret.txt",
			expectedStatus: http.StatusForbidden,
			shouldContain:  "Invalid resource path",
		},
		{
			name:           "ProxyResource - absolute path attempt",
			endpoint:       "/archive/123/20240309150405mp_/",
			path:           "resources/../../../etc/passwd",
			expectedStatus: http.StatusForbidden,
			shouldContain:  "Invalid resource path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()

			if tt.endpoint == "/archive/resources/" {
				router.GET("/archive/resources/*filepath", handler.ServeLocalResource)
			} else {
				router.GET("/archive/:page_id/:timestamp/*url", handler.ProxyResource)
			}

			req := httptest.NewRequest("GET", tt.endpoint+tt.path, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			if tt.shouldContain != "" && w.Body.String() != tt.shouldContain {
				t.Errorf("Expected body to contain %q, got %q", tt.shouldContain, w.Body.String())
			}
		})
	}
}

// TestValidateResourcePath 测试路径验证函数
func TestValidateResourcePath(t *testing.T) {
	tmpDir := t.TempDir()
	baseDir := filepath.Join(tmpDir, "resources")
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		t.Fatalf("Failed to create base dir: %v", err)
	}

	tests := []struct {
		name        string
		resourcePath string
		shouldError bool
	}{
		{
			name:         "valid relative path",
			resourcePath: "css/style.css",
			shouldError:  false,
		},
		{
			name:         "valid nested path",
			resourcePath: "images/icons/logo.png",
			shouldError:  false,
		},
		{
			name:         "path traversal with ../",
			resourcePath: "../secret.txt",
			shouldError:  true,
		},
		{
			name:         "path traversal with ../../",
			resourcePath: "../../etc/passwd",
			shouldError:  true,
		},
		{
			name:         "path traversal in middle",
			resourcePath: "css/../../../secret.txt",
			shouldError:  true,
		},
		{
			name:         "absolute path",
			resourcePath: "/etc/passwd",
			shouldError:  true,
		},
		{
			name:         "clean path with ..",
			resourcePath: "css/./style.css",
			shouldError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validateResourcePath(baseDir, tt.resourcePath)
			if tt.shouldError && err == nil {
				t.Errorf("Expected error for path %q, but got none", tt.resourcePath)
			}
			if !tt.shouldError && err != nil {
				t.Errorf("Expected no error for path %q, but got: %v", tt.resourcePath, err)
			}
		})
	}
}
