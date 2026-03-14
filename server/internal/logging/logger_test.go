package logging

import (
	"fmt"
	"os"
	"sync"
	"testing"
	"time"
)

// TestConcurrentWrites verifies that concurrent writes don't cause data races
func TestConcurrentWrites(t *testing.T) {
	tmpDir := t.TempDir()

	logger, err := Setup(tmpDir)
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	defer logger.Close()

	// Simulate concurrent writes from multiple goroutines
	const numGoroutines = 10
	const writesPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < writesPerGoroutine; j++ {
				msg := fmt.Sprintf("goroutine %d write %d\n", id, j)
				logger.writer.Write([]byte(msg))
			}
		}(i)
	}

	wg.Wait()

	// Verify curSize is updated correctly
	logger.mu.Lock()
	finalSize := logger.curSize
	logger.mu.Unlock()

	if finalSize <= 0 {
		t.Errorf("Expected curSize > 0, got %d", finalSize)
	}

	// Verify file size matches tracked size
	info, err := os.Stat(logger.file.Name())
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}

	if info.Size() != finalSize {
		t.Errorf("File size mismatch: tracked=%d, actual=%d", finalSize, info.Size())
	}
}

// TestConcurrentWriteAndRotate verifies no data race between Write and rotate
func TestConcurrentWriteAndRotate(t *testing.T) {
	tmpDir := t.TempDir()

	logger, err := Setup(tmpDir)
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	defer logger.Close()

	done := make(chan struct{})

	// Goroutine 1: continuous writes
	go func() {
		for {
			select {
			case <-done:
				return
			default:
				logger.writer.Write([]byte("test log message\n"))
				time.Sleep(1 * time.Millisecond)
			}
		}
	}()

	// Goroutine 2: trigger rotate multiple times
	go func() {
		for i := 0; i < 5; i++ {
			time.Sleep(10 * time.Millisecond)
			logger.rotate()
		}
		close(done)
	}()

	<-done
	time.Sleep(50 * time.Millisecond) // Let writes finish
}

// TestSizeTracking verifies curSize is accurately tracked
func TestSizeTracking(t *testing.T) {
	tmpDir := t.TempDir()

	logger, err := Setup(tmpDir)
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	defer logger.Close()

	testData := []string{
		"first line\n",
		"second line with more content\n",
		"third line\n",
	}

	var expectedSize int64
	for _, data := range testData {
		n, err := logger.writer.Write([]byte(data))
		if err != nil {
			t.Fatalf("Write failed: %v", err)
		}
		expectedSize += int64(n)
	}

	logger.mu.Lock()
	actualSize := logger.curSize
	logger.mu.Unlock()

	if actualSize != expectedSize {
		t.Errorf("Size mismatch: expected=%d, actual=%d", expectedSize, actualSize)
	}
}

// TestRotationResetSize verifies curSize is reset correctly after rotation
func TestRotationResetSize(t *testing.T) {
	tmpDir := t.TempDir()

	logger, err := Setup(tmpDir)
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	defer logger.Close()

	// Write some data
	logger.writer.Write([]byte("initial data\n"))

	logger.mu.Lock()
	initialSize := logger.curSize
	logger.mu.Unlock()

	if initialSize == 0 {
		t.Fatal("Expected initialSize > 0")
	}

	// Force rotation by changing date
	logger.mu.Lock()
	logger.curDate = "2020-01-01" // Old date to trigger rotation
	logger.mu.Unlock()

	err = logger.rotate()
	if err != nil {
		t.Fatalf("Rotation failed: %v", err)
	}

	// Write new data
	newData := "new data after rotation\n"
	n, _ := logger.writer.Write([]byte(newData))

	logger.mu.Lock()
	newSize := logger.curSize
	logger.mu.Unlock()

	// After rotation, curSize should be close to the new write size
	if newSize < int64(n) {
		t.Errorf("Expected curSize >= %d after rotation, got %d", n, newSize)
	}
}

// TestSizeRotationResetsSize verifies that curSize is reset to 0 when rotating
// due to size limit, preventing repeated creation of empty log files.
func TestSizeRotationResetsSize(t *testing.T) {
	tmpDir := t.TempDir()

	logger, err := Setup(tmpDir)
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	defer logger.Close()

	firstFile := logger.file.Name()

	// Simulate file exceeding maxLogSize by setting curSize directly
	logger.mu.Lock()
	logger.curSize = maxLogSize + 1
	logger.mu.Unlock()

	// First rotate: should create a new file
	if err := logger.rotate(); err != nil {
		t.Fatalf("First rotate failed: %v", err)
	}

	secondFile := logger.file.Name()
	if secondFile == firstFile {
		t.Fatal("Expected rotation to open a new file")
	}

	// curSize should be 0 after rotating to a brand-new file
	logger.mu.Lock()
	sizeAfterRotate := logger.curSize
	logger.mu.Unlock()

	if sizeAfterRotate != 0 {
		t.Errorf("Expected curSize=0 after size rotation, got %d", sizeAfterRotate)
	}

	// Second rotate: curSize is 0, should NOT rotate again
	if err := logger.rotate(); err != nil {
		t.Fatalf("Second rotate failed: %v", err)
	}

	thirdFile := logger.file.Name()
	if thirdFile != secondFile {
		t.Errorf("Second rotate should not have created a new file: got %s, want %s", thirdFile, secondFile)
	}
}

// TestRepeatedSizeRotationNoEmptyFiles verifies that triggering rotate()
// multiple times after a size-based rotation does not create empty files.
// This is the exact scenario that caused 118 empty log files in production.
func TestRepeatedSizeRotationNoEmptyFiles(t *testing.T) {
	tmpDir := t.TempDir()

	logger, err := Setup(tmpDir)
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	defer logger.Close()

	// Simulate file exceeding maxLogSize
	logger.mu.Lock()
	logger.curSize = maxLogSize + 1
	logger.mu.Unlock()

	// Rotate once (creates new file)
	if err := logger.rotate(); err != nil {
		t.Fatalf("Rotate failed: %v", err)
	}

	fileAfterRotate := logger.file.Name()

	// Simulate 10 more ticker-driven rotations (every 10s in production)
	for i := 0; i < 10; i++ {
		if err := logger.rotate(); err != nil {
			t.Fatalf("Rotate %d failed: %v", i, err)
		}
	}

	// Should still be on the same file
	if logger.file.Name() != fileAfterRotate {
		t.Errorf("Unexpected file change: got %s, want %s", logger.file.Name(), fileAfterRotate)
	}

	// Count log files in tmpDir
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	logCount := 0
	for _, e := range entries {
		if !e.IsDir() {
			logCount++
		}
	}
	// Should be exactly 2: the initial file (from Setup) + one rotation
	// The 10 subsequent rotate() calls should NOT create any new files
	if logCount != 2 {
		t.Errorf("Expected 2 log files, got %d", logCount)
	}
}
