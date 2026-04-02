package storage

import (
	"fmt"
	"sync"
	"testing"

	"wayback/internal/config"
)

// TestCacheStore_ConcurrentStress 高并发压力测试
// 验证 cacheMu 互斥锁确保并发 cacheStore 不会导致 cacheBytes 超出限制
func TestCacheStore_ConcurrentStress(t *testing.T) {
	// 1MB 缓存，500 个 goroutine 各写 10KB
	d := newTestDeduplicator(1)
	maxBytes := d.cacheMaxBytes()

	var wg sync.WaitGroup
	const goroutines = 500
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			key := fmt.Sprintf("resource-%d", i)
			data := make([]byte, 10*1024) // 10KB
			d.cacheStore(key, int64(i), fmt.Sprintf("resources/%02x/%02x/hash.bin", i%256, (i/256)%256), data)
		}(i)
	}

	wg.Wait()

	// cacheBytes 必须 <= 缓存上限
	actual := d.cacheBytes.Load()
	if actual > maxBytes {
		t.Errorf("cacheBytes = %d, exceeds limit %d (leaked %d bytes)", actual, maxBytes, actual-maxBytes)
	}
	if actual < 0 {
		t.Errorf("cacheBytes = %d, should not be negative", actual)
	}
}

// TestCacheStore_ConcurrentOverwrite 并发覆盖同一 key 不丢失计数
func TestCacheStore_ConcurrentOverwrite(t *testing.T) {
	d := newTestDeduplicator(100) // 100MB，不触发淘汰

	var wg sync.WaitGroup
	const goroutines = 200
	wg.Add(goroutines)

	// 所有 goroutine 写同一个 key
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			data := make([]byte, 1024) // 1KB
			d.cacheStore("same-key", int64(i), "resources/path.bin", data)
		}(i)
	}

	wg.Wait()

	// 最终 cacheBytes 应该恰好等于 1KB（只有一个 key）
	expected := int64(1024)
	actual := d.cacheBytes.Load()
	if actual != expected {
		t.Errorf("cacheBytes = %d, want %d (concurrent overwrites should track correctly)", actual, expected)
	}
}

// TestCacheStore_ConcurrentEvictionAccuracy 验证并发淘汰后 cacheBytes 不为负
func TestCacheStore_ConcurrentEvictionAccuracy(t *testing.T) {
	// 非常小的缓存，强制频繁淘汰
	d := &Deduplicator{
		config: config.ResourceConfig{
			Workers:     2,
			CacheSizeMB: 0, // 0MB → cacheMaxBytes() = 0，每次写入都会触发淘汰
		},
	}
	// 0MB 意味着 entrySize > 0 总是 > cacheMaxBytes()，所以什么都不会被缓存
	// 改为 1MB 但是写入大量数据
	d = newTestDeduplicator(1)

	var wg sync.WaitGroup
	const goroutines = 100
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			// 每个 50KB，100 个 = 5MB >> 1MB，大量淘汰
			key := fmt.Sprintf("key-%d", i)
			data := make([]byte, 50*1024)
			d.cacheStore(key, int64(i), "", data)
		}(i)
	}

	wg.Wait()

	actual := d.cacheBytes.Load()
	if actual < 0 {
		t.Errorf("cacheBytes = %d, should not be negative after concurrent eviction", actual)
	}
	if actual > d.cacheMaxBytes() {
		t.Errorf("cacheBytes = %d, exceeds limit %d after concurrent eviction", actual, d.cacheMaxBytes())
	}
}
