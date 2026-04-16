package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"wayback/internal/storage"
)

func TestArchivedCSSFontSubresource_PreservesWOFF2ContentType(t *testing.T) {
	gin.SetMode(gin.TestMode)

	dataDir := t.TempDir()
	fs := storage.NewFileStorage(dataDir)
	hash := strings.Repeat("a", 64)

	// Minimal WOFF2 signature is enough for this regression test.
	fontData := append([]byte("wOF2"), []byte("test-font-payload")...)
	relPath, err := fs.SaveResource(fontData, hash, "font")
	if err != nil {
		t.Fatalf("SaveResource() error = %v", err)
	}

	parser := storage.NewCSSParser()
	css := `@font-face { src: url("../fonts/app.woff2") format("woff2"); }`
	rewritten := rewriteCSSForPage(parser, css, "https://example.com/assets/css/app.css", func(resourceURL string) (string, bool) {
		if resourceURL == "https://example.com/assets/fonts/app.woff2" {
			return relPath, true
		}
		return "", false
	})

	archivedURL := "/archive/" + relPath
	if !strings.Contains(rewritten, archivedURL) {
		t.Fatalf("rewritten CSS = %q, want %q", rewritten, archivedURL)
	}
	if !strings.HasSuffix(relPath, ".font") {
		t.Fatalf("saved font path = %q, want synthetic .font extension for reproduction", relPath)
	}

	handler := &Handler{dataDir: dataDir}
	router := gin.New()
	router.GET("/archive/resources/*filepath", handler.ServeLocalResource)

	req := httptest.NewRequest(http.MethodGet, archivedURL, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	contentType := w.Header().Get("Content-Type")
	if !strings.Contains(contentType, "font/woff2") {
		t.Fatalf("Content-Type = %q, want font/woff2 for archived WOFF2 font", contentType)
	}
}
