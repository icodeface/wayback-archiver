package storage

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// DeletionRecord 记录待删除的 HTML 文件信息
type DeletionRecord struct {
	HTMLPath  string    `json:"html_path"`  // 相对路径，如 "html/2026/03/14/120000_abc123.html"
	Timestamp time.Time `json:"timestamp"`  // 记录时间
	PageID    int64     `json:"page_id"`    // 关联的页面 ID（用于日志）
}

// DeletionQueue 管理待删除的 HTML 文件队列
type DeletionQueue struct {
	queueFile string
	mu        sync.Mutex
}

// NewDeletionQueue 创建删除队列管理器
func NewDeletionQueue(dataDir string) *DeletionQueue {
	queueFile := filepath.Join(dataDir, "deletion_queue.jsonl")
	return &DeletionQueue{
		queueFile: queueFile,
	}
}

// Add 添加一个待删除的 HTML 文件记录
func (q *DeletionQueue) Add(htmlPath string, pageID int64) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	record := DeletionRecord{
		HTMLPath:  htmlPath,
		Timestamp: time.Now(),
		PageID:    pageID,
	}

	// 追加到文件
	f, err := os.OpenFile(q.queueFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open deletion queue: %w", err)
	}
	defer f.Close()

	encoder := json.NewEncoder(f)
	if err := encoder.Encode(record); err != nil {
		return fmt.Errorf("failed to write deletion record: %w", err)
	}

	return nil
}

// ProcessDeletions 处理删除队列，删除超过 retentionDays 的文件
// 记录按时间顺序追加，遇到第一个未过期的记录即可停止，剩余部分直接保留
// 返回删除的文件数量和错误
func (q *DeletionQueue) ProcessDeletions(baseDir string, retentionDays int) (int, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	records, err := q.readRecords()
	if err != nil {
		return 0, err
	}

	if len(records) == 0 {
		return 0, nil
	}

	cutoffTime := time.Now().AddDate(0, 0, -retentionDays)
	deletedCount := 0
	firstRemaining := -1

	for i, record := range records {
		if !record.Timestamp.Before(cutoffTime) {
			// 记录按时间有序，后续全部未过期，直接截断
			firstRemaining = i
			break
		}

		fullPath := filepath.Join(baseDir, record.HTMLPath)
		if err := os.Remove(fullPath); err != nil {
			if !os.IsNotExist(err) {
				// 删除失败（权限等问题），保留这条记录
				if firstRemaining == -1 {
					firstRemaining = i
				}
				fmt.Printf("Failed to delete %s: %v\n", record.HTMLPath, err)
				break // 后续记录也保留
			}
			// 文件不存在，视为已删除
		} else {
			deletedCount++
			fmt.Printf("Deleted superseded HTML: %s (page_id: %d, age: %v)\n",
				record.HTMLPath, record.PageID, time.Since(record.Timestamp))
		}
	}

	// 保留未处理的记录
	var remaining []DeletionRecord
	if firstRemaining >= 0 {
		remaining = records[firstRemaining:]
	}

	if err := q.writeRecords(remaining); err != nil {
		return deletedCount, fmt.Errorf("failed to update deletion queue: %w", err)
	}

	return deletedCount, nil
}

// readRecords 读取所有删除记录
func (q *DeletionQueue) readRecords() ([]DeletionRecord, error) {
	f, err := os.Open(q.queueFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to open deletion queue: %w", err)
	}
	defer f.Close()

	var records []DeletionRecord
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var record DeletionRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			// 跳过损坏的行
			fmt.Printf("Warning: skipping invalid deletion record: %v\n", err)
			continue
		}
		records = append(records, record)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read deletion queue: %w", err)
	}

	return records, nil
}

// writeRecords 重写队列文件
func (q *DeletionQueue) writeRecords(records []DeletionRecord) error {
	// 写入临时文件
	tempFile := q.queueFile + ".tmp"
	f, err := os.Create(tempFile)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}

	encoder := json.NewEncoder(f)
	for _, record := range records {
		if err := encoder.Encode(record); err != nil {
			f.Close()
			os.Remove(tempFile)
			return fmt.Errorf("failed to write record: %w", err)
		}
	}

	if err := f.Close(); err != nil {
		os.Remove(tempFile)
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	// 原子替换
	if err := os.Rename(tempFile, q.queueFile); err != nil {
		os.Remove(tempFile)
		return fmt.Errorf("failed to replace queue file: %w", err)
	}

	return nil
}
