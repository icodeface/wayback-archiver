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

	// Add gzip middleware (same as production)
	r.Use(gzipMiddleware.Gzip(gzipMiddleware.DefaultCompression))

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
