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
