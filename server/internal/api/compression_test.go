package api

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	gzipMiddleware "github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
	"wayback/internal/config"
)

// setupRouterWithGzip creates a test router with gzip middleware
func setupRouterWithGzip() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())

	// Add gzip middleware with request decompression support (same as production)
	r.Use(gzipMiddleware.Gzip(gzipMiddleware.DefaultCompression, gzipMiddleware.WithDecompressFn(gzipMiddleware.DefaultDecompressHandle)))

	handler := &Handler{} // minimal handler
	authCfg := &config.AuthConfig{Password: ""}
	SetupRoutes(r, handler, authCfg, "test", "")

	return r
}

func TestGzipCompression_Enabled(t *testing.T) {
	r := setupRouterWithGzip()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/version", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	r.ServeHTTP(w, req)

	// Check Content-Encoding header
	contentEncoding := w.Header().Get("Content-Encoding")
	if contentEncoding != "gzip" {
		t.Errorf("expected Content-Encoding: gzip, got %q", contentEncoding)
	}

	// Check Vary header (important for caching)
	vary := w.Header().Get("Vary")
	if !strings.Contains(vary, "Accept-Encoding") {
		t.Errorf("expected Vary header to contain Accept-Encoding, got %q", vary)
	}
}

func TestGzipCompression_WithoutAcceptEncoding(t *testing.T) {
	r := setupRouterWithGzip()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/version", nil)
	// No Accept-Encoding header
	r.ServeHTTP(w, req)

	// Should not compress
	contentEncoding := w.Header().Get("Content-Encoding")
	if contentEncoding == "gzip" {
		t.Error("should not compress without Accept-Encoding: gzip header")
	}
}

func TestGzipCompression_DecompressesCorrectly(t *testing.T) {
	r := setupRouterWithGzip()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/version", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	r.ServeHTTP(w, req)

	if w.Header().Get("Content-Encoding") != "gzip" {
		t.Fatal("response not gzipped")
	}

	// Decompress the response
	reader, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatalf("failed to create gzip reader: %v", err)
	}
	defer reader.Close()

	decompressed, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("failed to decompress: %v", err)
	}

	// Should be valid JSON
	body := string(decompressed)
	if !strings.Contains(body, "version") || !strings.Contains(body, "build_time") {
		t.Errorf("decompressed body doesn't look like version response: %s", body)
	}
}

func TestGzipCompression_ReducesSize(t *testing.T) {
	r := setupRouterWithGzip()

	// Request without compression
	w1 := httptest.NewRecorder()
	req1, _ := http.NewRequest("GET", "/api/version", nil)
	r.ServeHTTP(w1, req1)
	uncompressedSize := w1.Body.Len()

	// Request with compression
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("GET", "/api/version", nil)
	req2.Header.Set("Accept-Encoding", "gzip")
	r.ServeHTTP(w2, req2)
	compressedSize := w2.Body.Len()

	// For small responses, gzip might not reduce size (overhead)
	// But for larger responses, it should compress
	// We just verify that compression was attempted
	if w2.Header().Get("Content-Encoding") != "gzip" {
		t.Error("compression not applied when Accept-Encoding: gzip is set")
	}

	t.Logf("Uncompressed: %d bytes, Compressed: %d bytes", uncompressedSize, compressedSize)
}

func TestGzipCompression_LargeResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gzipMiddleware.Gzip(gzipMiddleware.DefaultCompression))

	// Create a route that returns a large JSON response
	largeData := strings.Repeat(`{"key":"value","data":"Lorem ipsum dolor sit amet"}`, 100)
	r.GET("/large", func(c *gin.Context) {
		c.String(http.StatusOK, largeData)
	})

	// Request without compression
	w1 := httptest.NewRecorder()
	req1, _ := http.NewRequest("GET", "/large", nil)
	r.ServeHTTP(w1, req1)
	uncompressedSize := w1.Body.Len()

	// Request with compression
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("GET", "/large", nil)
	req2.Header.Set("Accept-Encoding", "gzip")
	r.ServeHTTP(w2, req2)
	compressedSize := w2.Body.Len()

	// For large repetitive text, compression should significantly reduce size
	if compressedSize >= uncompressedSize {
		t.Errorf("compression didn't reduce size: uncompressed=%d, compressed=%d",
			uncompressedSize, compressedSize)
	}

	compressionRatio := float64(compressedSize) / float64(uncompressedSize) * 100
	t.Logf("Compression ratio: %.1f%% (uncompressed: %d bytes, compressed: %d bytes)",
		compressionRatio, uncompressedSize, compressedSize)

	// Should achieve at least 50% compression for repetitive text
	if compressionRatio > 50 {
		t.Errorf("compression ratio too low: %.1f%% (expected < 50%%)", compressionRatio)
	}
}

func TestGzipCompression_POSTRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gzipMiddleware.Gzip(gzipMiddleware.DefaultCompression))

	// Create a POST endpoint that returns a large response
	r.POST("/api/archive", func(c *gin.Context) {
		// Return a large JSON response to ensure compression
		largeResponse := strings.Repeat(`{"status":"ok","message":"archived"}`, 50)
		c.String(http.StatusOK, largeResponse)
	})

	body := bytes.NewBufferString(`{"url":"https://example.com","title":"Test"}`)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/archive", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Encoding", "gzip")
	r.ServeHTTP(w, req)

	// Response should be compressed
	contentEncoding := w.Header().Get("Content-Encoding")
	if contentEncoding != "gzip" {
		t.Errorf("POST response not compressed, Content-Encoding: %q", contentEncoding)
	}
}

func TestGzipCompression_MultipleEncodings(t *testing.T) {
	r := setupRouterWithGzip()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/version", nil)
	// Client supports multiple encodings
	req.Header.Set("Accept-Encoding", "gzip, deflate, br")
	r.ServeHTTP(w, req)

	// Should use gzip (first supported encoding)
	contentEncoding := w.Header().Get("Content-Encoding")
	if contentEncoding != "gzip" {
		t.Errorf("expected gzip encoding, got %q", contentEncoding)
	}
}

// Test request body decompression
func TestGzipDecompression_RequestBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gzipMiddleware.Gzip(gzipMiddleware.DefaultCompression, gzipMiddleware.WithDecompressFn(gzipMiddleware.DefaultDecompressHandle)))

	// Create a route that echoes back the request body
	var receivedBody string
	r.POST("/echo", func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.String(http.StatusBadRequest, "failed to read body")
			return
		}
		receivedBody = string(body)
		c.String(http.StatusOK, receivedBody)
	})

	// Compress the request body
	originalData := `{"url":"https://example.com","title":"Test Page","html":"<html><body>Large content here...</body></html>"}`
	var compressedBuf bytes.Buffer
	gzipWriter := gzip.NewWriter(&compressedBuf)
	_, err := gzipWriter.Write([]byte(originalData))
	if err != nil {
		t.Fatalf("failed to compress data: %v", err)
	}
	gzipWriter.Close()

	// Send compressed request
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/echo", &compressedBuf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	// Server should have decompressed and received the original data
	if receivedBody != originalData {
		t.Errorf("server received wrong data.\nExpected: %s\nGot: %s", originalData, receivedBody)
	}

	t.Logf("Original size: %d bytes, Compressed size: %d bytes (%.1f%% reduction)",
		len(originalData), compressedBuf.Len(),
		(1-float64(compressedBuf.Len())/float64(len(originalData)))*100)
}

func TestGzipDecompression_LargeRequestBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gzipMiddleware.Gzip(gzipMiddleware.DefaultCompression, gzipMiddleware.WithDecompressFn(gzipMiddleware.DefaultDecompressHandle)))

	var receivedSize int
	r.POST("/upload", func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.String(http.StatusBadRequest, "failed to read body")
			return
		}
		receivedSize = len(body)
		c.String(http.StatusOK, "ok")
	})

	// Create a large payload (simulating a large HTML snapshot)
	largeData := strings.Repeat(`<div class="content">Lorem ipsum dolor sit amet, consectetur adipiscing elit.</div>`, 500)
	originalSize := len(largeData)

	// Compress it
	var compressedBuf bytes.Buffer
	gzipWriter := gzip.NewWriter(&compressedBuf)
	gzipWriter.Write([]byte(largeData))
	gzipWriter.Close()
	compressedSize := compressedBuf.Len()

	// Send compressed request
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/upload", &compressedBuf)
	req.Header.Set("Content-Encoding", "gzip")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	// Server should have received the full decompressed data
	if receivedSize != originalSize {
		t.Errorf("server received %d bytes, expected %d bytes", receivedSize, originalSize)
	}

	compressionRatio := (1 - float64(compressedSize)/float64(originalSize)) * 100
	t.Logf("Request compression: %d bytes → %d bytes (%.1f%% reduction)",
		originalSize, compressedSize, compressionRatio)

	// Should achieve significant compression for repetitive HTML
	if compressionRatio < 50 {
		t.Errorf("compression ratio too low: %.1f%% (expected >= 50%%)", compressionRatio)
	}
}

func TestGzipDecompression_WithoutContentEncoding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gzipMiddleware.Gzip(gzipMiddleware.DefaultCompression, gzipMiddleware.WithDecompressFn(gzipMiddleware.DefaultDecompressHandle)))

	var receivedBody string
	r.POST("/echo", func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		receivedBody = string(body)
		c.String(http.StatusOK, receivedBody)
	})

	// Send uncompressed request (no Content-Encoding header)
	originalData := `{"test":"data"}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/echo", strings.NewReader(originalData))
	req.Header.Set("Content-Type", "application/json")
	// No Content-Encoding header
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	// Should pass through uncompressed data unchanged
	if receivedBody != originalData {
		t.Errorf("uncompressed data was modified.\nExpected: %s\nGot: %s", originalData, receivedBody)
	}
}

// Test mixed scenario: uncompressed request, compressed response
func TestMixedCompression_UncompressedRequestCompressedResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	// Request decompression middleware (always active)
	r.Use(func(c *gin.Context) {
		if c.Request.Header.Get("Content-Encoding") == "gzip" {
			gzipMiddleware.DefaultDecompressHandle(c)
		}
		c.Next()
	})

	// Response compression middleware (configurable)
	r.Use(gzipMiddleware.Gzip(gzipMiddleware.DefaultCompression))

	var receivedBody string
	r.POST("/api/archive", func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		receivedBody = string(body)
		c.JSON(http.StatusOK, gin.H{"status": "success", "received_size": len(body)})
	})

	// Send uncompressed request
	testData := `{"url":"https://example.com","title":"Test"}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/archive", strings.NewReader(testData))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Encoding", "gzip")
	// No Content-Encoding header (uncompressed request)
	r.ServeHTTP(w, req)

	// Server should receive uncompressed data
	if receivedBody != testData {
		t.Errorf("server received wrong data.\nExpected: %s\nGot: %s", testData, receivedBody)
	}

	// Response should be compressed
	if w.Header().Get("Content-Encoding") != "gzip" {
		t.Error("response should be compressed")
	}
}

// Test configuration: compression disabled on server
func TestCompressionDisabled_ServerSide(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	// Request decompression middleware (always active)
	r.Use(func(c *gin.Context) {
		if c.Request.Header.Get("Content-Encoding") == "gzip" {
			gzipMiddleware.DefaultDecompressHandle(c)
		}
		c.Next()
	})

	// Response compression disabled (no gzip middleware)

	var receivedBody string
	r.POST("/api/archive", func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		receivedBody = string(body)
		c.JSON(http.StatusOK, gin.H{"status": "success"})
	})

	// Client sends compressed request
	testData := `{"url":"https://example.com","title":"Test"}`
	var compressedBuf bytes.Buffer
	gzipWriter := gzip.NewWriter(&compressedBuf)
	gzipWriter.Write([]byte(testData))
	gzipWriter.Close()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/archive", &compressedBuf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	req.Header.Set("Accept-Encoding", "gzip")
	r.ServeHTTP(w, req)

	// Server should decompress and receive original data
	if receivedBody != testData {
		t.Errorf("server failed to decompress.\nExpected: %s\nGot: %s", testData, receivedBody)
	}

	// Response should NOT be compressed (compression disabled)
	if w.Header().Get("Content-Encoding") == "gzip" {
		t.Error("response should not be compressed when compression is disabled")
	}
}

// Test configuration: both client and server compression enabled
func TestCompressionEnabled_BothSides(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	// Request decompression middleware
	r.Use(func(c *gin.Context) {
		if c.Request.Header.Get("Content-Encoding") == "gzip" {
			gzipMiddleware.DefaultDecompressHandle(c)
		}
		c.Next()
	})

	// Response compression middleware
	r.Use(gzipMiddleware.Gzip(gzipMiddleware.DefaultCompression))

	var receivedBody string
	r.POST("/api/archive", func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		receivedBody = string(body)
		// Return large response to ensure compression
		largeResponse := strings.Repeat(`{"status":"ok"}`, 100)
		c.String(http.StatusOK, largeResponse)
	})

	// Client sends compressed request
	testData := `{"url":"https://example.com","title":"Test","html":"` + strings.Repeat("<div>content</div>", 100) + `"}`
	var compressedBuf bytes.Buffer
	gzipWriter := gzip.NewWriter(&compressedBuf)
	gzipWriter.Write([]byte(testData))
	gzipWriter.Close()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/archive", &compressedBuf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	req.Header.Set("Accept-Encoding", "gzip")
	r.ServeHTTP(w, req)

	// Server should decompress request
	if receivedBody != testData {
		t.Error("server failed to decompress request")
	}

	// Response should be compressed
	if w.Header().Get("Content-Encoding") != "gzip" {
		t.Error("response should be compressed")
	}

	t.Logf("Request: %d bytes (compressed: %d bytes)", len(testData), compressedBuf.Len())
	t.Logf("Response compressed: %d bytes", w.Body.Len())
}

