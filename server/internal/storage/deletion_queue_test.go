package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func newTestQueue(t *testing.T) (*DeletionQueue, string) {
	t.Helper()
	dir := t.TempDir()
	return NewDeletionQueue(dir), dir
}

// createHTMLFile 在 baseDir 下创建一个 HTML 文件，返回相对路径
func createHTMLFile(t *testing.T, baseDir, relPath string) {
	t.Helper()
	fullPath := filepath.Join(baseDir, relPath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(fullPath, []byte("<html>test</html>"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestDeletionQueue_Add(t *testing.T) {
	q, dir := newTestQueue(t)

	if err := q.Add("html/2026/03/14/old.html", 1); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := q.Add("html/2026/03/14/old2.html", 2); err != nil {
		t.Fatalf("Add second: %v", err)
	}

	// 验证文件内容
	data, err := os.ReadFile(filepath.Join(dir, "deletion_queue.jsonl"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	lines := splitNonEmpty(string(data))
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}

	var rec DeletionRecord
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if rec.HTMLPath != "html/2026/03/14/old.html" {
		t.Errorf("HTMLPath = %q, want %q", rec.HTMLPath, "html/2026/03/14/old.html")
	}
	if rec.PageID != 1 {
		t.Errorf("PageID = %d, want 1", rec.PageID)
	}
}

func TestDeletionQueue_ProcessDeletions_DeletesExpiredFiles(t *testing.T) {
	q, dir := newTestQueue(t)
	baseDir := filepath.Join(dir, "data")

	// 创建 HTML 文件
	createHTMLFile(t, baseDir, "html/old.html")

	// 手动写入一条 8 天前的记录
	rec := DeletionRecord{
		HTMLPath:  "html/old.html",
		Timestamp: time.Now().AddDate(0, 0, -8),
		PageID:    10,
	}
	f, _ := os.Create(q.queueFile)
	json.NewEncoder(f).Encode(rec)
	f.Close()

	// 执行清理（保留 7 天）
	deleted, err := q.ProcessDeletions(baseDir, 7)
	if err != nil {
		t.Fatalf("ProcessDeletions: %v", err)
	}
	if deleted != 1 {
		t.Errorf("deleted = %d, want 1", deleted)
	}

	// 验证文件已删除
	if _, err := os.Stat(filepath.Join(baseDir, "html/old.html")); !os.IsNotExist(err) {
		t.Error("file should have been deleted")
	}

	// 验证队列文件已清空
	records := readQueueFile(t, q.queueFile)
	if len(records) != 0 {
		t.Errorf("queue should be empty, got %d records", len(records))
	}
}

func TestDeletionQueue_ProcessDeletions_KeepsRecentFiles(t *testing.T) {
	q, dir := newTestQueue(t)
	baseDir := filepath.Join(dir, "data")

	// 创建 HTML 文件
	createHTMLFile(t, baseDir, "html/recent.html")

	// 写入一条 3 天前的记录（未过期）
	rec := DeletionRecord{
		HTMLPath:  "html/recent.html",
		Timestamp: time.Now().AddDate(0, 0, -3),
		PageID:    20,
	}
	f, _ := os.Create(q.queueFile)
	json.NewEncoder(f).Encode(rec)
	f.Close()

	// 执行清理（保留 7 天）
	deleted, err := q.ProcessDeletions(baseDir, 7)
	if err != nil {
		t.Fatalf("ProcessDeletions: %v", err)
	}
	if deleted != 0 {
		t.Errorf("deleted = %d, want 0", deleted)
	}

	// 验证文件仍然存在
	if _, err := os.Stat(filepath.Join(baseDir, "html/recent.html")); err != nil {
		t.Error("file should still exist")
	}

	// 验证队列中仍有记录
	records := readQueueFile(t, q.queueFile)
	if len(records) != 1 {
		t.Errorf("queue should have 1 record, got %d", len(records))
	}
}

func TestDeletionQueue_ProcessDeletions_MixedExpiry(t *testing.T) {
	q, dir := newTestQueue(t)
	baseDir := filepath.Join(dir, "data")

	// 创建两个文件
	createHTMLFile(t, baseDir, "html/old.html")
	createHTMLFile(t, baseDir, "html/recent.html")

	// 写入两条记录：一条过期，一条未过期
	f, _ := os.Create(q.queueFile)
	enc := json.NewEncoder(f)
	enc.Encode(DeletionRecord{HTMLPath: "html/old.html", Timestamp: time.Now().AddDate(0, 0, -10), PageID: 1})
	enc.Encode(DeletionRecord{HTMLPath: "html/recent.html", Timestamp: time.Now().AddDate(0, 0, -2), PageID: 2})
	f.Close()

	deleted, err := q.ProcessDeletions(baseDir, 7)
	if err != nil {
		t.Fatalf("ProcessDeletions: %v", err)
	}
	if deleted != 1 {
		t.Errorf("deleted = %d, want 1", deleted)
	}

	// old.html 应该被删除
	if _, err := os.Stat(filepath.Join(baseDir, "html/old.html")); !os.IsNotExist(err) {
		t.Error("old.html should have been deleted")
	}
	// recent.html 应该保留
	if _, err := os.Stat(filepath.Join(baseDir, "html/recent.html")); err != nil {
		t.Error("recent.html should still exist")
	}

	// 队列中只剩 recent.html
	records := readQueueFile(t, q.queueFile)
	if len(records) != 1 {
		t.Fatalf("queue should have 1 record, got %d", len(records))
	}
	if records[0].HTMLPath != "html/recent.html" {
		t.Errorf("remaining record = %q, want %q", records[0].HTMLPath, "html/recent.html")
	}
}

func TestDeletionQueue_ProcessDeletions_FileAlreadyGone(t *testing.T) {
	q, dir := newTestQueue(t)
	baseDir := filepath.Join(dir, "data")

	// 不创建文件，直接写入过期记录
	rec := DeletionRecord{
		HTMLPath:  "html/ghost.html",
		Timestamp: time.Now().AddDate(0, 0, -10),
		PageID:    99,
	}
	f, _ := os.Create(q.queueFile)
	json.NewEncoder(f).Encode(rec)
	f.Close()

	// 文件不存在时不应报错，记录应被移除
	deleted, err := q.ProcessDeletions(baseDir, 7)
	if err != nil {
		t.Fatalf("ProcessDeletions: %v", err)
	}
	if deleted != 0 {
		t.Errorf("deleted = %d, want 0 (file didn't exist)", deleted)
	}

	// 队列应为空（记录已移除）
	records := readQueueFile(t, q.queueFile)
	if len(records) != 0 {
		t.Errorf("queue should be empty, got %d records", len(records))
	}
}

func TestDeletionQueue_ProcessDeletions_EmptyQueue(t *testing.T) {
	q, dir := newTestQueue(t)
	baseDir := filepath.Join(dir, "data")

	// 队列文件不存在
	deleted, err := q.ProcessDeletions(baseDir, 7)
	if err != nil {
		t.Fatalf("ProcessDeletions: %v", err)
	}
	if deleted != 0 {
		t.Errorf("deleted = %d, want 0", deleted)
	}
}

func TestDeletionQueue_ProcessDeletions_CorruptedLine(t *testing.T) {
	q, dir := newTestQueue(t)
	baseDir := filepath.Join(dir, "data")

	createHTMLFile(t, baseDir, "html/valid.html")

	// 写入一行损坏数据 + 一行有效过期记录
	f, _ := os.Create(q.queueFile)
	f.WriteString("this is not json\n")
	json.NewEncoder(f).Encode(DeletionRecord{
		HTMLPath:  "html/valid.html",
		Timestamp: time.Now().AddDate(0, 0, -10),
		PageID:    5,
	})
	f.Close()

	// 应跳过损坏行，正常处理有效记录
	deleted, err := q.ProcessDeletions(baseDir, 7)
	if err != nil {
		t.Fatalf("ProcessDeletions: %v", err)
	}
	if deleted != 1 {
		t.Errorf("deleted = %d, want 1", deleted)
	}
}

func TestDeletionQueue_ConcurrentAdd(t *testing.T) {
	q, _ := newTestQueue(t)

	var wg sync.WaitGroup
	n := 50
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			q.Add("html/page.html", int64(i))
		}(i)
	}
	wg.Wait()

	records := readQueueFile(t, q.queueFile)
	if len(records) != n {
		t.Errorf("expected %d records, got %d", n, len(records))
	}
}

// --- helpers ---

func splitNonEmpty(s string) []string {
	var lines []string
	for _, line := range splitLines(s) {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func readQueueFile(t *testing.T, path string) []DeletionRecord {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("ReadFile: %v", err)
	}
	var records []DeletionRecord
	for _, line := range splitNonEmpty(string(data)) {
		var rec DeletionRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		records = append(records, rec)
	}
	return records
}
