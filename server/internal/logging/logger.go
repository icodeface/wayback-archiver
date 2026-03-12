package logging

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	filePrefix    = "wayback-"
	fileSuffix    = ".log"
	dateFormat    = "2006-01-02"
	retentionDays = 7
	maxLogSize    = 10 * 1024 * 1024 // 10MB per file
)

// Logger manages log file rotation and cleanup.
type Logger struct {
	dir      string
	mu       sync.Mutex
	file     *os.File
	curDate  string
	curSeq   int
	curSize  int64
	stopCh   chan struct{}
	writer   io.Writer
}

// Setup initializes the logging system: creates log dir, opens today's
// log file, redirects stdlib log + gin output, and starts the cleanup goroutine.
func Setup(logDir string) (*Logger, error) {
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("create log dir: %w", err)
	}

	l := &Logger{dir: logDir, stopCh: make(chan struct{})}
	if err := l.rotate(); err != nil {
		return nil, err
	}

	// Clean old logs on startup
	l.cleanup()

	// Background: rotate at midnight + periodic cleanup
	go l.backgroundLoop()

	return l, nil
}

// Close stops the background goroutine and closes the current log file.
func (l *Logger) Close() {
	close(l.stopCh)
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		l.file.Close()
	}
}

// Dir returns the log directory path.
func (l *Logger) Dir() string {
	return l.dir
}

// rotate opens (or switches to) today's log file.
func (l *Logger) rotate() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	today := time.Now().Format(dateFormat)

	// Check if we need to rotate due to date change or size limit
	needRotate := false
	if today != l.curDate {
		// New day, reset sequence
		l.curDate = today
		l.curSeq = 1
		needRotate = true
	} else if l.file != nil && l.curSize >= maxLogSize {
		// Same day but file too large, increment sequence
		l.curSeq++
		needRotate = true
	} else if l.file == nil {
		// No file open yet
		needRotate = true
	}

	if !needRotate {
		return nil
	}

	if l.file != nil {
		l.file.Close()
	}

	// Find next available sequence number for today
	for {
		filename := l.getFilename(today, l.curSeq)
		info, err := os.Stat(filename)
		if os.IsNotExist(err) {
			// File doesn't exist, use this sequence
			break
		}
		if err != nil {
			return fmt.Errorf("stat log file: %w", err)
		}
		// File exists, check if it's under size limit
		if info.Size() < maxLogSize {
			// Can append to this file
			l.curSize = info.Size()
			break
		}
		// File is full, try next sequence
		l.curSeq++
	}

	filename := l.getFilename(today, l.curSeq)
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}

	l.file = f

	// Create a writer that tracks size
	l.writer = &sizeTrackingWriter{
		writer: f,
		size:   &l.curSize,
	}

	// Redirect stdlib log and gin to both stdout and file
	mw := io.MultiWriter(os.Stdout, l.writer)
	log.SetOutput(mw)
	gin.DefaultWriter = mw
	gin.DefaultErrorWriter = io.MultiWriter(os.Stderr, l.writer)

	return nil
}

func (l *Logger) getFilename(date string, seq int) string {
	return filepath.Join(l.dir, fmt.Sprintf("%s%s.%03d%s", filePrefix, date, seq, fileSuffix))
}

// sizeTrackingWriter wraps an io.Writer and tracks bytes written
type sizeTrackingWriter struct {
	writer io.Writer
	size   *int64
}

func (w *sizeTrackingWriter) Write(p []byte) (n int, err error) {
	n, err = w.writer.Write(p)
	if n > 0 {
		*w.size += int64(n)
	}
	return
}

// cleanup removes log files older than retentionDays.
func (l *Logger) cleanup() {
	cutoff := time.Now().AddDate(0, 0, -retentionDays)

	entries, err := os.ReadDir(l.dir)
	if err != nil {
		log.Printf("[logging] failed to read log dir: %v", err)
		return
	}

	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, filePrefix) || !strings.HasSuffix(name, fileSuffix) {
			continue
		}
		// Extract date from filename: wayback-2026-03-12.001.log -> 2026-03-12
		dateStr := strings.TrimPrefix(name, filePrefix)
		dateStr = strings.TrimSuffix(dateStr, fileSuffix)
		// Remove sequence number: 2026-03-12.001 -> 2026-03-12
		if idx := strings.LastIndex(dateStr, "."); idx > 0 {
			dateStr = dateStr[:idx]
		}
		t, err := time.Parse(dateFormat, dateStr)
		if err != nil {
			continue
		}
		if t.Before(cutoff) {
			path := filepath.Join(l.dir, name)
			if err := os.Remove(path); err != nil {
				log.Printf("[logging] failed to remove old log %s: %v", name, err)
			} else {
				log.Printf("[logging] removed old log: %s", name)
			}
		}
	}
}

// backgroundLoop handles daily rotation and cleanup.
func (l *Logger) backgroundLoop() {
	// Check for size-based rotation every 10 seconds
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		now := time.Now()
		// Next midnight
		next := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 1, 0, now.Location())
		timer := time.NewTimer(next.Sub(now))

		select {
		case <-ticker.C:
			// Check if current file needs rotation due to size
			if err := l.rotate(); err != nil {
				log.Printf("[logging] rotation failed: %v", err)
			}
		case <-timer.C:
			// Midnight rotation
			if err := l.rotate(); err != nil {
				log.Printf("[logging] rotation failed: %v", err)
			}
			l.cleanup()
		case <-l.stopCh:
			timer.Stop()
			return
		}
	}
}

// ListLogFiles returns metadata about available log files, sorted newest first.
type LogFileInfo struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	Date string `json:"date"`
}

func (l *Logger) ListLogFiles() ([]LogFileInfo, error) {
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		return nil, err
	}

	var files []LogFileInfo
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, filePrefix) || !strings.HasSuffix(name, fileSuffix) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		// Extract date: wayback-2026-03-12.001.log -> 2026-03-12
		dateStr := strings.TrimPrefix(name, filePrefix)
		dateStr = strings.TrimSuffix(dateStr, fileSuffix)
		if idx := strings.LastIndex(dateStr, "."); idx > 0 {
			dateStr = dateStr[:idx]
		}
		files = append(files, LogFileInfo{
			Name: name,
			Size: info.Size(),
			Date: dateStr,
		})
	}

	// Sort by filename (which includes date and sequence)
	sort.Slice(files, func(i, j int) bool {
		return files[i].Name > files[j].Name
	})
	return files, nil
}

// ReadLogFile reads the last N lines of a log file.
func (l *Logger) ReadLogFile(filename string, tail int) (string, error) {
	// Sanitize filename to prevent path traversal
	if strings.Contains(filename, "/") || strings.Contains(filename, "\\") || strings.Contains(filename, "..") {
		return "", fmt.Errorf("invalid filename")
	}
	if !strings.HasPrefix(filename, filePrefix) || !strings.HasSuffix(filename, fileSuffix) {
		return "", fmt.Errorf("invalid log filename")
	}

	path := filepath.Join(l.dir, filename)

	// Check for symlink to prevent symlink attacks
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("symlink not allowed")
	}

	// Limit file size to prevent OOM attacks (now per-file limit is 10MB, so this is extra safety)
	const maxReadSize = 50 * 1024 * 1024 // 50MB
	if info.Size() > maxReadSize {
		return "", fmt.Errorf("log file too large")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	content := string(data)
	if tail <= 0 {
		tail = 500
	}
	if tail > 10000 { // Limit max lines to prevent excessive memory usage
		tail = 10000
	}

	lines := strings.Split(content, "\n")
	if len(lines) > tail {
		lines = lines[len(lines)-tail:]
	}
	return strings.Join(lines, "\n"), nil
}
