package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestProxyResource_RewritesCSSForCurrentPage(t *testing.T) {
	handler, cleanup := setupTestHandler(t)
	defer cleanup()

	router := gin.New()
	router.GET("/archive/:page_id/:timestamp/*resource_path", handler.ProxyResource)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	pageURL := "https://proxy-css-test.example.com/page-" + suffix
	cssURL := "https://proxy-css-test.example.com/assets/css/app.css?v=1"
	importURL := "https://proxy-css-test.example.com/assets/css/theme.css?v=9#dark"
	imgURL := "https://proxy-css-test.example.com/assets/img/logo.png?size=2x"

	pageID, err := handler.db.CreatePage(pageURL, "Proxy CSS Test", "html/test/proxy.html", strings.Repeat("a", 64), time.Now())
	if err != nil {
		t.Fatalf("CreatePage failed: %v", err)
	}
	defer handler.db.DeletePage(pageID)

	cssPath := "resources/aa/bb/proxy-app.css"
	importPath := "resources/cc/dd/proxy-theme.css"
	imgPath := "resources/ee/ff/proxy-logo.img"

	writeTestResourceFile(t, handler.dataDir, cssPath, []byte(`@import url("./theme.css?v=9#dark"); .hero{background:url("../img/logo.png?size=2x")} .missing{background:url("../img/missing.png")}`))
	writeTestResourceFile(t, handler.dataDir, importPath, []byte(`body{color:#333}`))
	writeTestResourceFile(t, handler.dataDir, imgPath, []byte("img"))

	cssID, err := handler.db.CreateResource(cssURL, strings.Repeat("b", 64), "css", cssPath, 120)
	if err != nil {
		t.Fatalf("CreateResource css failed: %v", err)
	}
	importID, err := handler.db.CreateResource(importURL, strings.Repeat("c", 64), "css", importPath, 80)
	if err != nil {
		t.Fatalf("CreateResource import failed: %v", err)
	}
	imgID, err := handler.db.CreateResource(imgURL, strings.Repeat("d", 64), "image", imgPath, 3)
	if err != nil {
		t.Fatalf("CreateResource image failed: %v", err)
	}

	for _, resID := range []int64{cssID, importID, imgID} {
		if err := handler.db.LinkPageResource(pageID, resID); err != nil {
			t.Fatalf("LinkPageResource(%d) failed: %v", resID, err)
		}
	}

	urlPath := fmt.Sprintf("/archive/%d/20260410120000mp_/%s", pageID, cssURL)
	req := httptest.NewRequest(http.MethodGet, urlPath, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/css") {
		t.Fatalf("Content-Type = %q, want text/css", ct)
	}
	body := w.Body.String()
	if !strings.Contains(body, `@import url("/archive/resources/cc/dd/proxy-theme.css")`) {
		t.Fatalf("import URL not rewritten: %s", body)
	}
	if !strings.Contains(body, `url("/archive/resources/ee/ff/proxy-logo.img")`) {
		t.Fatalf("image URL not rewritten: %s", body)
	}
	if !strings.Contains(body, `url("../img/missing.png")`) {
		t.Fatalf("unmatched URL should remain original: %s", body)
	}

	storedCSS, err := os.ReadFile(filepath.Join(handler.dataDir, cssPath))
	if err != nil {
		t.Fatalf("ReadFile stored CSS failed: %v", err)
	}
	if strings.Contains(string(storedCSS), "/archive/resources/") {
		t.Fatalf("stored CSS should remain unmodified, got: %s", string(storedCSS))
	}
}

func TestProxyResource_SharedCSSDoesNotRewriteUnlinkedSubresourcesFromOtherPage(t *testing.T) {
	handler, cleanup := setupTestHandler(t)
	defer cleanup()

	router := gin.New()
	router.GET("/archive/:page_id/:timestamp/*resource_path", handler.ProxyResource)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	cssURL := "https://shared-css-test.example.com/assets/css/app.css"
	imgURL := "https://shared-css-test.example.com/assets/img/logo.png"

	page1ID, err := handler.db.CreatePage("https://shared-css-test.example.com/page-a-"+suffix, "Page A", "html/test/page-a.html", strings.Repeat("e", 64), time.Now())
	if err != nil {
		t.Fatalf("CreatePage page1 failed: %v", err)
	}
	defer handler.db.DeletePage(page1ID)

	page2ID, err := handler.db.CreatePage("https://shared-css-test.example.com/page-b-"+suffix, "Page B", "html/test/page-b.html", strings.Repeat("f", 64), time.Now())
	if err != nil {
		t.Fatalf("CreatePage page2 failed: %v", err)
	}
	defer handler.db.DeletePage(page2ID)

	cssPath := "resources/10/20/shared-app.css"
	imgPath := "resources/30/40/shared-logo.img"
	writeTestResourceFile(t, handler.dataDir, cssPath, []byte(`.hero{background:url("../img/logo.png")}`))
	writeTestResourceFile(t, handler.dataDir, imgPath, []byte("img"))

	cssID, err := handler.db.CreateResource(cssURL, strings.Repeat("1", 64), "css", cssPath, 64)
	if err != nil {
		t.Fatalf("CreateResource css failed: %v", err)
	}
	imgID, err := handler.db.CreateResource(imgURL, strings.Repeat("2", 64), "image", imgPath, 3)
	if err != nil {
		t.Fatalf("CreateResource image failed: %v", err)
	}

	if err := handler.db.LinkPageResource(page1ID, cssID); err != nil {
		t.Fatalf("LinkPageResource page1 css failed: %v", err)
	}
	if err := handler.db.LinkPageResource(page1ID, imgID); err != nil {
		t.Fatalf("LinkPageResource page1 image failed: %v", err)
	}
	if err := handler.db.LinkPageResource(page2ID, cssID); err != nil {
		t.Fatalf("LinkPageResource page2 css failed: %v", err)
	}

	req1 := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/archive/%d/20260410121000mp_/%s", page1ID, cssURL), nil)
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("page1 expected 200, got %d: %s", w1.Code, w1.Body.String())
	}
	if !strings.Contains(w1.Body.String(), `url("/archive/resources/30/40/shared-logo.img")`) {
		t.Fatalf("page1 should rewrite linked image, got: %s", w1.Body.String())
	}

	req2 := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/archive/%d/20260410121000mp_/%s", page2ID, cssURL), nil)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("page2 expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
	if strings.Contains(w2.Body.String(), "/archive/resources/30/40/shared-logo.img") {
		t.Fatalf("page2 should not rewrite subresource linked only to page1, got: %s", w2.Body.String())
	}
	if !strings.Contains(w2.Body.String(), `url("../img/logo.png")`) {
		t.Fatalf("page2 should keep original relative URL, got: %s", w2.Body.String())
	}
}

func TestProxyResource_EncodedCSSSubresourceDoesNotLeakAcrossPages(t *testing.T) {
	handler, cleanup := setupTestHandler(t)
	defer cleanup()

	router := gin.New()
	router.GET("/archive/:page_id/:timestamp/*resource_path", handler.ProxyResource)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	cssURL := "https://encoded-css-test.example.com/assets/css/app.css"
	imgURL := "https://encoded-css-test.example.com/assets/img/icon%20space.png"

	page1ID, err := handler.db.CreatePage("https://encoded-css-test.example.com/page-a-"+suffix, "Page A", "html/test/encoded-a.html", strings.Repeat("7", 64), time.Now())
	if err != nil {
		t.Fatalf("CreatePage page1 failed: %v", err)
	}
	defer handler.db.DeletePage(page1ID)

	page2ID, err := handler.db.CreatePage("https://encoded-css-test.example.com/page-b-"+suffix, "Page B", "html/test/encoded-b.html", strings.Repeat("8", 64), time.Now())
	if err != nil {
		t.Fatalf("CreatePage page2 failed: %v", err)
	}
	defer handler.db.DeletePage(page2ID)

	cssPath := "resources/41/42/encoded-app.css"
	imgPath := "resources/43/44/encoded-icon.img"
	writeTestResourceFile(t, handler.dataDir, cssPath, []byte(`.icon{background:url("../img/icon space.png")}`))
	writeTestResourceFile(t, handler.dataDir, imgPath, []byte("img"))

	cssID, err := handler.db.CreateResource(cssURL, strings.Repeat("9", 64), "css", cssPath, 64)
	if err != nil {
		t.Fatalf("CreateResource css failed: %v", err)
	}
	imgID, err := handler.db.CreateResource(imgURL, strings.Repeat("a", 64), "image", imgPath, 3)
	if err != nil {
		t.Fatalf("CreateResource image failed: %v", err)
	}

	if err := handler.db.LinkPageResource(page1ID, cssID); err != nil {
		t.Fatalf("LinkPageResource page1 css failed: %v", err)
	}
	if err := handler.db.LinkPageResource(page1ID, imgID); err != nil {
		t.Fatalf("LinkPageResource page1 image failed: %v", err)
	}
	if err := handler.db.LinkPageResource(page2ID, cssID); err != nil {
		t.Fatalf("LinkPageResource page2 css failed: %v", err)
	}

	req1 := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/archive/%d/20260410122000mp_/%s", page1ID, cssURL), nil)
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("page1 expected 200, got %d: %s", w1.Code, w1.Body.String())
	}
	if !strings.Contains(w1.Body.String(), `url("/archive/resources/43/44/encoded-icon.img")`) {
		t.Fatalf("page1 should rewrite encoded linked image, got: %s", w1.Body.String())
	}

	req2 := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/archive/%d/20260410122000mp_/%s", page2ID, cssURL), nil)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("page2 expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
	if strings.Contains(w2.Body.String(), "/archive/resources/43/44/encoded-icon.img") {
		t.Fatalf("page2 should not rewrite encoded subresource linked only to page1, got: %s", w2.Body.String())
	}
	if !strings.Contains(w2.Body.String(), `url("../img/icon space.png")`) {
		t.Fatalf("page2 should keep original encoded relative URL source, got: %s", w2.Body.String())
	}
}

func writeTestResourceFile(t *testing.T, dataDir, relPath string, content []byte) {
	t.Helper()
	absPath := filepath.Join(dataDir, relPath)
	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(absPath, content, 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
}
