package storage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
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

func TestDownloadToFile_SmallData(t *testing.T) {
	fs := NewFileStorage(t.TempDir())
	content := []byte("hello world from downloadToFile")

	data, hash, tmpPath, err := fs.downloadToFile(bytes.NewReader(content))
	if err != nil {
		t.Fatalf("downloadToFile failed: %v", err)
	}

	// data 应为 nil（流式写入磁盘）
	if data != nil {
		t.Errorf("expected nil data, got %d bytes", len(data))
	}

	// hash 应正确
	expected := sha256.Sum256(content)
	expectedHash := hex.EncodeToString(expected[:])
	if hash != expectedHash {
		t.Errorf("hash = %s, want %s", hash, expectedHash)
	}

	// tmpPath 应存在且内容正确
	if tmpPath == "" {
		t.Fatal("expected non-empty tmpPath")
	}
	saved, err := os.ReadFile(tmpPath)
	if err != nil {
		t.Fatalf("failed to read tmp file: %v", err)
	}
	if !bytes.Equal(saved, content) {
		t.Errorf("tmp file content mismatch")
	}

	// 清理
	os.Remove(tmpPath)
}

func TestDownloadToFile_EmptyReader(t *testing.T) {
	fs := NewFileStorage(t.TempDir())

	data, hash, tmpPath, err := fs.downloadToFile(bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("downloadToFile failed: %v", err)
	}
	if data != nil {
		t.Error("expected nil data")
	}
	if hash == "" {
		t.Error("expected non-empty hash for empty content")
	}
	if tmpPath == "" {
		t.Fatal("expected non-empty tmpPath")
	}

	// 文件应为空
	info, _ := os.Stat(tmpPath)
	if info.Size() != 0 {
		t.Errorf("expected empty file, got %d bytes", info.Size())
	}

	os.Remove(tmpPath)
}

func TestDownloadToFile_ErrorReader(t *testing.T) {
	fs := NewFileStorage(t.TempDir())

	// 读取时返回错误的 reader
	errReader := &errorAfterReader{data: []byte("partial"), failAfter: 3}
	_, _, tmpPath, err := fs.downloadToFile(errReader)

	if err == nil {
		t.Error("expected error from downloadToFile with failing reader")
	}
	// 出错时应清理临时文件（defer 中 os.Remove）
	if tmpPath != "" {
		if _, statErr := os.Stat(tmpPath); !os.IsNotExist(statErr) {
			t.Error("expected tmp file to be cleaned up on error")
			os.Remove(tmpPath)
		}
	}
}

func TestDownloadBuffered_SmallFile(t *testing.T) {
	fs := NewFileStorage(t.TempDir())
	content := []byte("small content within threshold")
	threshold := int64(1024) // 1KB 阈值

	data, hash, tmpPath, err := fs.downloadBuffered(bytes.NewReader(content), threshold)
	if err != nil {
		t.Fatalf("downloadBuffered failed: %v", err)
	}

	// 小文件应留在内存
	if data == nil {
		t.Fatal("expected data in memory for small file")
	}
	if !bytes.Equal(data, content) {
		t.Error("data content mismatch")
	}

	// hash 应正确
	expected := sha256.Sum256(content)
	expectedHash := hex.EncodeToString(expected[:])
	if hash != expectedHash {
		t.Errorf("hash = %s, want %s", hash, expectedHash)
	}

	// 不应有临时文件
	if tmpPath != "" {
		t.Errorf("expected no tmpPath for small file, got %s", tmpPath)
	}
}

func TestDownloadBuffered_LargeFile(t *testing.T) {
	fs := NewFileStorage(t.TempDir())
	content := make([]byte, 5*1024) // 5KB
	for i := range content {
		content[i] = byte(i % 256)
	}
	threshold := int64(1024) // 1KB 阈值

	data, hash, tmpPath, err := fs.downloadBuffered(bytes.NewReader(content), threshold)
	if err != nil {
		t.Fatalf("downloadBuffered failed: %v", err)
	}

	// 大文件应溢出到磁盘
	if data != nil {
		t.Error("expected nil data for large file (should be on disk)")
	}

	// hash 应正确
	expected := sha256.Sum256(content)
	expectedHash := hex.EncodeToString(expected[:])
	if hash != expectedHash {
		t.Errorf("hash = %s, want %s", hash, expectedHash)
	}

	// tmpPath 应存在且内容正确
	if tmpPath == "" {
		t.Fatal("expected non-empty tmpPath for large file")
	}
	saved, err := os.ReadFile(tmpPath)
	if err != nil {
		t.Fatalf("failed to read tmp file: %v", err)
	}
	if !bytes.Equal(saved, content) {
		t.Error("tmp file content mismatch")
	}

	os.Remove(tmpPath)
}

func TestDownloadBuffered_ExactlyThresholdPlusOne(t *testing.T) {
	fs := NewFileStorage(t.TempDir())
	threshold := int64(100)
	// 恰好 threshold+1 字节，应溢出到磁盘
	content := make([]byte, threshold+1)
	for i := range content {
		content[i] = byte('A' + i%26)
	}

	data, hash, tmpPath, err := fs.downloadBuffered(bytes.NewReader(content), threshold)
	if err != nil {
		t.Fatalf("downloadBuffered failed: %v", err)
	}

	if data != nil {
		t.Error("expected nil data when size = threshold+1")
	}
	if tmpPath == "" {
		t.Fatal("expected tmpPath when size = threshold+1")
	}

	// 验证内容完整性
	saved, _ := os.ReadFile(tmpPath)
	if !bytes.Equal(saved, content) {
		t.Error("tmp file content mismatch at boundary")
	}

	// 验证哈希
	expected := sha256.Sum256(content)
	if hash != hex.EncodeToString(expected[:]) {
		t.Error("hash mismatch at boundary")
	}

	os.Remove(tmpPath)
}

func TestDownloadBuffered_ExactlyThreshold(t *testing.T) {
	fs := NewFileStorage(t.TempDir())
	threshold := int64(100)
	// 恰好 threshold 字节，应留在内存
	content := make([]byte, threshold)
	for i := range content {
		content[i] = byte('X')
	}

	data, _, tmpPath, err := fs.downloadBuffered(bytes.NewReader(content), threshold)
	if err != nil {
		t.Fatalf("downloadBuffered failed: %v", err)
	}

	if data == nil {
		t.Fatal("expected data in memory when size = threshold")
	}
	if tmpPath != "" {
		t.Error("expected no tmpPath when size = threshold")
		os.Remove(tmpPath)
	}
	if !bytes.Equal(data, content) {
		t.Error("data content mismatch at exact threshold")
	}
}

func TestDownloadBuffered_EmptyReader(t *testing.T) {
	fs := NewFileStorage(t.TempDir())

	data, hash, tmpPath, err := fs.downloadBuffered(bytes.NewReader(nil), 1024)
	if err != nil {
		t.Fatalf("downloadBuffered failed on empty: %v", err)
	}

	// 空内容应留在内存
	if data == nil {
		t.Error("expected non-nil data slice for empty content")
	}
	if len(data) != 0 {
		t.Errorf("expected empty data, got %d bytes", len(data))
	}
	if tmpPath != "" {
		t.Error("expected no tmpPath for empty content")
	}
	if hash == "" {
		t.Error("expected non-empty hash for empty content")
	}
}

func TestCopyFile(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "src.txt")
	dstPath := filepath.Join(dir, "dst.txt")

	content := []byte("copy file test content")
	if err := os.WriteFile(srcPath, content, 0644); err != nil {
		t.Fatalf("failed to write src: %v", err)
	}

	if err := copyFile(srcPath, dstPath); err != nil {
		t.Fatalf("copyFile failed: %v", err)
	}

	// 验证目标文件内容
	dstData, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("failed to read dst: %v", err)
	}
	if !bytes.Equal(dstData, content) {
		t.Error("dst content mismatch")
	}

	// 验证源文件仍在
	if _, err := os.Stat(srcPath); err != nil {
		t.Error("src file should still exist after copy")
	}
}

func TestCopyFile_SrcNotExist(t *testing.T) {
	dir := t.TempDir()
	err := copyFile(filepath.Join(dir, "nonexistent"), filepath.Join(dir, "dst"))
	if err == nil {
		t.Error("expected error for non-existent source")
	}
}

func TestNewFileStorage_DefaultTimeout(t *testing.T) {
	fs := NewFileStorage(t.TempDir())
	if fs.httpClient.Timeout.Seconds() != 30 {
		t.Errorf("default timeout = %v, want 30s", fs.httpClient.Timeout)
	}
}

func TestNewFileStorage_CustomTimeout(t *testing.T) {
	fs := NewFileStorage(t.TempDir(), 60)
	if fs.httpClient.Timeout.Seconds() != 60 {
		t.Errorf("custom timeout = %v, want 60s", fs.httpClient.Timeout)
	}
}

func TestNewFileStorage_ZeroTimeoutUsesDefault(t *testing.T) {
	fs := NewFileStorage(t.TempDir(), 0)
	// 0 不满足 downloadTimeout[0] > 0，使用默认值 30
	if fs.httpClient.Timeout.Seconds() != 30 {
		t.Errorf("zero timeout = %v, want 30s (default)", fs.httpClient.Timeout)
	}
}

func TestGetExtension(t *testing.T) {
	tests := []struct {
		resourceType string
		expected     string
	}{
		{"image", ".img"},
		{"css", ".css"},
		{"js", ".js"},
		{"font", ".font"},
		{"other", ".bin"},
		{"video", ".bin"},
		{"", ".bin"},
	}

	for _, tt := range tests {
		ext := getExtension(tt.resourceType)
		if ext != tt.expected {
			t.Errorf("getExtension(%q) = %q, want %q", tt.resourceType, ext, tt.expected)
		}
	}
}

// errorAfterReader 读取前 failAfter 个字节后返回错误
type errorAfterReader struct {
	data      []byte
	failAfter int
	read      int
}

func (r *errorAfterReader) Read(p []byte) (n int, err error) {
	if r.read >= r.failAfter {
		return 0, io.ErrUnexpectedEOF
	}
	remaining := r.failAfter - r.read
	if remaining > len(p) {
		remaining = len(p)
	}
	if remaining > len(r.data)-r.read {
		remaining = len(r.data) - r.read
	}
	if remaining <= 0 {
		return 0, io.ErrUnexpectedEOF
	}
	copy(p, r.data[r.read:r.read+remaining])
	r.read += remaining
	return remaining, nil
}

func TestDownloadBuffered_ErrorDuringSpill(t *testing.T) {
	fs := NewFileStorage(t.TempDir())
	threshold := int64(10)

	// 创建一个会在读取超过 threshold 后返回错误的 reader
	// 先提供 threshold+1 字节（触发溢出），然后在后续读取中出错
	prefix := make([]byte, threshold+1)
	for i := range prefix {
		prefix[i] = 'A'
	}

	errReader := io.MultiReader(
		bytes.NewReader(prefix),
		&failingReader{},
	)

	_, _, tmpPath, err := fs.downloadBuffered(errReader, threshold)
	if err == nil {
		t.Error("expected error during spill to disk")
	}
	// 临时文件应已清理
	if tmpPath != "" {
		if _, statErr := os.Stat(tmpPath); !os.IsNotExist(statErr) {
			os.Remove(tmpPath)
			t.Error("expected tmp file to be cleaned up on error")
		}
	}
}

// failingReader 总是返回错误
type failingReader struct{}

func (r *failingReader) Read(p []byte) (n int, err error) {
	return 0, io.ErrUnexpectedEOF
}
