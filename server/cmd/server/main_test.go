package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"wayback/internal/api"
	"wayback/internal/config"
	"wayback/internal/database"
	"wayback/internal/models"
	"wayback/internal/storage"
)

type fakeBackgroundTaskWaiter struct {
	done <-chan struct{}
}

func (f fakeBackgroundTaskWaiter) WaitForBackgroundTasks() {
	if f.done != nil {
		<-f.done
	}
}

func TestWaitForBackgroundTasks_CompletesBeforeDeadline(t *testing.T) {
	done := make(chan struct{})
	close(done)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := waitForBackgroundTasks(ctx, fakeBackgroundTaskWaiter{done: done}); err != nil {
		t.Fatalf("waitForBackgroundTasks() error = %v, want nil", err)
	}
}

func TestWaitForBackgroundTasks_StopsOnContextDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := waitForBackgroundTasks(ctx, fakeBackgroundTaskWaiter{done: make(chan struct{})})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waitForBackgroundTasks() error = %v, want %v", err, context.DeadlineExceeded)
	}
}

func testDBUser() string {
	if user := os.Getenv("DB_USER"); user != "" {
		return user
	}
	if user := os.Getenv("USER"); user != "" {
		return user
	}
	return "postgres"
}

func newIntegrationTestDeduplicator(t *testing.T) (*storage.Deduplicator, *database.DB, string) {
	t.Helper()

	db, err := database.New("localhost", "5432", testDBUser(), "", "wayback")
	if err != nil {
		t.Skipf("Skipping integration test (cannot connect to DB): %v", err)
	}

	dataDir := t.TempDir()
	proxyURL := os.Getenv("http_proxy")
	proxyWasSet := proxyURL != ""

	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(250 * time.Millisecond)
		w.Header().Set("Content-Type", "text/css")
		_, _ = w.Write([]byte("body { color: rgb(12, 34, 56); }"))
	}))
	t.Cleanup(proxy.Close)

	if err := os.Setenv("http_proxy", proxy.URL); err != nil {
		t.Fatalf("Setenv(http_proxy) failed: %v", err)
	}
	t.Cleanup(func() {
		if proxyWasSet {
			_ = os.Setenv("http_proxy", proxyURL)
			return
		}
		_ = os.Unsetenv("http_proxy")
	})

	fs := storage.NewFileStorage(dataDir, 1)
	if proxyWasSet {
		if err := os.Setenv("http_proxy", proxyURL); err != nil {
			t.Fatalf("restore http_proxy failed: %v", err)
		}
	} else {
		if err := os.Unsetenv("http_proxy"); err != nil {
			t.Fatalf("Unsetenv(http_proxy) failed: %v", err)
		}
	}

	dedup := storage.NewDeduplicator(db, fs, config.ResourceConfig{
		Workers:           2,
		MetadataCacheMB:   10,
		DownloadTimeout:   1,
		StreamThresholdKB: 2048,
	})

	t.Cleanup(func() {
		dedup.WaitForBackgroundTasks()
		db.Close()
	})

	return dedup, db, dataDir
}

func TestServeWithGracefulShutdown_WaitsForAsyncArchiveFinalize(t *testing.T) {
	dedup, db, dataDir := newIntegrationTestDeduplicator(t)
	handler := api.NewHandler(dedup, db, dataDir, nil)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	apiGroup := router.Group("/api")
	apiGroup.POST("/archive", handler.ArchivePage)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen failed: %v", err)
	}
	defer listener.Close()

	server := &http.Server{Handler: router}
	serveCtx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- serveWithGracefulShutdown(serveCtx, server, dedup, func() error {
			return server.Serve(listener)
		})
	}()

	pageURL := fmt.Sprintf("https://graceful-shutdown.example/page-%d", time.Now().UnixNano())
	cssURL := "http://archive-test.invalid/slow.css"
	reqBody, err := json.Marshal(models.CaptureRequest{
		URL:   pageURL,
		Title: "graceful shutdown archive",
		HTML:  `<html><head><link rel="stylesheet" href="` + cssURL + `"></head><body>shutdown integration</body></html>`,
	})
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	client := &http.Client{Transport: &http.Transport{Proxy: nil}}
	resp, err := client.Post("http://"+listener.Addr().String()+"/api/archive", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		cancel()
		t.Fatalf("POST /api/archive failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		cancel()
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var archiveResp models.ArchiveResponse
	if err := json.NewDecoder(resp.Body).Decode(&archiveResp); err != nil {
		cancel()
		t.Fatalf("decode response failed: %v", err)
	}
	if archiveResp.PageID <= 0 {
		cancel()
		t.Fatalf("page_id = %d, want positive", archiveResp.PageID)
	}
	defer db.DeletePage(archiveResp.PageID)

	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("serveWithGracefulShutdown returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for graceful shutdown to finish")
	}

	resources, err := db.GetResourcesByPageID(archiveResp.PageID)
	if err != nil {
		t.Fatalf("GetResourcesByPageID failed: %v", err)
	}
	if len(resources) != 1 {
		t.Fatalf("expected 1 linked resource after graceful shutdown, got %d", len(resources))
	}
	if resources[0].URL != cssURL {
		t.Fatalf("resource URL = %q, want %q", resources[0].URL, cssURL)
	}

	page, err := db.GetPageByID(strconv.FormatInt(archiveResp.PageID, 10))
	if err != nil {
		t.Fatalf("GetPageByID failed: %v", err)
	}
	if page == nil {
		t.Fatalf("expected page %d to exist", archiveResp.PageID)
	}

	htmlContent, err := os.ReadFile(filepath.Join(dataDir, page.HTMLPath))
	if err != nil {
		t.Fatalf("ReadFile page html failed: %v", err)
	}

	expectedProxyURL := fmt.Sprintf("/archive/%d/%smp_/%s", archiveResp.PageID, page.CapturedAt.Format("20060102150405"), cssURL)
	if !strings.Contains(string(htmlContent), expectedProxyURL) {
		t.Fatalf("page HTML should contain rewritten CSS proxy URL %q, got: %s", expectedProxyURL, string(htmlContent))
	}
}
