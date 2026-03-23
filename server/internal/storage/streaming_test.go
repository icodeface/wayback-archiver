package storage

import (
	"os"
	"path/filepath"
	"testing"

	"wayback/internal/config"
	"wayback/internal/database"
)

func TestDownloadResource_SmallFileInMemory(t *testing.T) {
	t.Skip("SSRF protection blocks localhost - tested via integration tests")
}

func TestDownloadResource_LargeFileStreaming(t *testing.T) {
	t.Skip("SSRF protection blocks localhost - tested via integration tests")
}

func TestDownloadResource_ThresholdBoundary(t *testing.T) {
	t.Skip("SSRF protection blocks localhost - tested via integration tests")
}

func TestSaveResourceFromFile(t *testing.T) {
	fs := NewFileStorage(t.TempDir())

	// 创建临时文件
	tmpDir := filepath.Join(fs.baseDir, "tmp")
	os.MkdirAll(tmpDir, 0755)
	tmpFile, err := os.CreateTemp(tmpDir, "test-*.tmp")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	testData := []byte("test content for save from file")
	tmpFile.Write(testData)
	tmpFile.Close()
	tmpPath := tmpFile.Name()

	// 保存到资源目录
	hash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	relPath, err := fs.SaveResourceFromFile(tmpPath, hash, "text")

	if err != nil {
		t.Fatalf("SaveResourceFromFile failed: %v", err)
	}
	if relPath == "" {
		t.Error("Expected relative path to be returned")
	}

	// 验证临时文件已被移走
	if _, statErr := os.Stat(tmpPath); !os.IsNotExist(statErr) {
		t.Error("Expected temp file to be removed after save")
	}

	// 验证资源文件存在且内容正确
	fullPath := filepath.Join(fs.baseDir, relPath)
	savedData, readErr := os.ReadFile(fullPath)
	if readErr != nil {
		t.Fatalf("Failed to read saved file: %v", readErr)
	}
	if string(savedData) != string(testData) {
		t.Errorf("Saved data mismatch: got %q, want %q", string(savedData), string(testData))
	}
}

func TestSaveResourceFromFile_AlreadyExists(t *testing.T) {
	fs := NewFileStorage(t.TempDir())

	// 先保存一个资源
	hash := "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"
	existingData := []byte("existing content")
	relPath1, err := fs.SaveResource(existingData, hash, "text")
	if err != nil {
		t.Fatalf("SaveResource failed: %v", err)
	}

	// 创建临时文件（内容不同）
	tmpDir := filepath.Join(fs.baseDir, "tmp")
	os.MkdirAll(tmpDir, 0755)
	tmpFile, _ := os.CreateTemp(tmpDir, "test-*.tmp")
	tmpFile.Write([]byte("new content"))
	tmpFile.Close()
	tmpPath := tmpFile.Name()

	// 尝试保存同哈希的文件
	relPath2, err := fs.SaveResourceFromFile(tmpPath, hash, "text")
	if err != nil {
		t.Fatalf("SaveResourceFromFile failed: %v", err)
	}

	// 应该返回已存在文件的路径
	if relPath2 != relPath1 {
		t.Errorf("Expected same path for duplicate hash: got %s, want %s", relPath2, relPath1)
	}

	// 临时文件应该被删除
	if _, statErr := os.Stat(tmpPath); !os.IsNotExist(statErr) {
		t.Error("Expected temp file to be removed when resource already exists")
	}

	// 原文件内容不应该被覆盖
	fullPath := filepath.Join(fs.baseDir, relPath1)
	savedData, _ := os.ReadFile(fullPath)
	if string(savedData) != string(existingData) {
		t.Error("Existing file should not be overwritten")
	}
}

func TestGlobalSemaphore(t *testing.T) {
	db, err := database.New("localhost", "5432", "apple", "", "wayback")
	if err != nil {
		t.Skip("PostgreSQL not available:", err)
	}
	defer db.Close()

	fs := NewFileStorage(t.TempDir())
	cfg := config.ResourceConfig{
		Workers:           3,
		CacheSizeMB:       10,
		DownloadTimeout:   30,
		StreamThresholdKB: 2048,
	}
	dedup := NewDeduplicator(db, fs, cfg)

	// 验证信号量容量等于配置的 Workers
	if cap(dedup.globalSem) != 3 {
		t.Errorf("Expected global semaphore capacity 3, got %d", cap(dedup.globalSem))
	}
}

func TestStreamThresholdKB_ZeroPassedThrough(t *testing.T) {
	db, err := database.New("localhost", "5432", "apple", "", "wayback")
	if err != nil {
		t.Skip("PostgreSQL not available:", err)
	}
	defer db.Close()

	fs := NewFileStorage(t.TempDir())
	cfg := config.ResourceConfig{
		Workers:           2,
		CacheSizeMB:       10,
		DownloadTimeout:   30,
		StreamThresholdKB: 0, // 0 意味着所有文件都流式落盘
	}
	dedup := NewDeduplicator(db, fs, cfg)

	if dedup.config.StreamThresholdKB != 0 {
		t.Errorf("StreamThresholdKB = %d, want 0 (should not be overridden)", dedup.config.StreamThresholdKB)
	}
}

func TestCleanupTmp_RemovesOrphanFiles(t *testing.T) {
	fs := NewFileStorage(t.TempDir())

	tmpDir := filepath.Join(fs.baseDir, "tmp")
	os.MkdirAll(tmpDir, 0755)

	// 创建几个模拟的孤儿临时文件
	for i := 0; i < 3; i++ {
		f, err := os.CreateTemp(tmpDir, "dl-*.tmp")
		if err != nil {
			t.Fatalf("Failed to create temp file: %v", err)
		}
		f.Write([]byte("orphan data"))
		f.Close()
	}

	n, err := fs.CleanupTmp()
	if err != nil {
		t.Fatalf("CleanupTmp failed: %v", err)
	}
	if n != 3 {
		t.Errorf("CleanupTmp removed %d files, want 3", n)
	}

	// 验证 tmp 目录现在为空
	entries, _ := os.ReadDir(tmpDir)
	if len(entries) != 0 {
		t.Errorf("tmp dir still has %d entries after cleanup", len(entries))
	}
}

func TestCleanupTmp_NoTmpDir(t *testing.T) {
	fs := NewFileStorage(t.TempDir())
	// tmp 目录不存在时应该返回 0, nil
	n, err := fs.CleanupTmp()
	if err != nil {
		t.Errorf("CleanupTmp error on non-existent dir: %v", err)
	}
	if n != 0 {
		t.Errorf("CleanupTmp removed %d files, want 0", n)
	}
}

func TestCleanupTmp_SkipsSubdirectories(t *testing.T) {
	fs := NewFileStorage(t.TempDir())

	tmpDir := filepath.Join(fs.baseDir, "tmp")
	os.MkdirAll(tmpDir, 0755)

	// 创建一个文件和一个子目录
	f, _ := os.CreateTemp(tmpDir, "dl-*.tmp")
	f.Close()
	os.MkdirAll(filepath.Join(tmpDir, "subdir"), 0755)

	n, err := fs.CleanupTmp()
	if err != nil {
		t.Fatalf("CleanupTmp failed: %v", err)
	}
	if n != 1 {
		t.Errorf("CleanupTmp removed %d files, want 1 (should skip subdirectories)", n)
	}

	// 子目录应该仍然存在
	if _, err := os.Stat(filepath.Join(tmpDir, "subdir")); os.IsNotExist(err) {
		t.Error("CleanupTmp should not remove subdirectories")
	}
}

func TestSaveResourceFromFile_ThenReadResource(t *testing.T) {
	fs := NewFileStorage(t.TempDir())

	// 模拟大 CSS 文件流式落盘后的场景：SaveResourceFromFile → ReadResource
	cssContent := "body { background: url('image.png'); } .icon { background-image: url('/fonts/icon.woff2'); }"
	hash := "ccccdddd1234567890abcdef1234567890abcdef1234567890abcdef12345678"

	tmpDir := filepath.Join(fs.baseDir, "tmp")
	os.MkdirAll(tmpDir, 0755)
	tmpFile, _ := os.CreateTemp(tmpDir, "dl-*.tmp")
	tmpFile.Write([]byte(cssContent))
	tmpFile.Close()

	relPath, err := fs.SaveResourceFromFile(tmpFile.Name(), hash, "css")
	if err != nil {
		t.Fatalf("SaveResourceFromFile failed: %v", err)
	}

	// 验证可以通过 ReadResource 读回内容（模拟大 CSS 子资源提取场景）
	data, err := fs.ReadResource(relPath)
	if err != nil {
		t.Fatalf("ReadResource failed: %v", err)
	}
	if string(data) != cssContent {
		t.Errorf("ReadResource content mismatch: got %q, want %q", string(data), cssContent)
	}
}
