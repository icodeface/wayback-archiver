package storage

import (
	"testing"
	"time"

	"wayback/internal/config"
)

func newTestDeduplicator(cacheSizeMB int) *Deduplicator {
	return &Deduplicator{
		config: config.ResourceConfig{
			Workers:     2,
			CacheSizeMB: cacheSizeMB,
		},
	}
}

func TestCacheStore_BasicStoreAndRetrieve(t *testing.T) {
	d := newTestDeduplicator(100) // 100MB cache

	data := []byte("hello world")
	d.cacheStore("key1", 42, data)

	entry, ok := d.cache.Load("key1")
	if !ok {
		t.Fatal("expected cache entry for key1")
	}

	cached := entry.(*resourceCacheEntry)
	if cached.resourceID != 42 {
		t.Errorf("resourceID = %d, want 42", cached.resourceID)
	}
	if string(cached.data) != "hello world" {
		t.Errorf("data = %q, want %q", string(cached.data), "hello world")
	}
	if d.cacheBytes.Load() != int64(len(data)) {
		t.Errorf("cacheBytes = %d, want %d", d.cacheBytes.Load(), len(data))
	}
}

func TestCacheStore_OverwriteUpdatesSize(t *testing.T) {
	d := newTestDeduplicator(100)

	d.cacheStore("key1", 1, []byte("short"))
	if d.cacheBytes.Load() != 5 {
		t.Fatalf("cacheBytes = %d, want 5", d.cacheBytes.Load())
	}

	// 覆盖同一个 key，数据更大
	d.cacheStore("key1", 1, []byte("much longer data"))
	if d.cacheBytes.Load() != 16 {
		t.Errorf("cacheBytes = %d, want 16 (after overwrite)", d.cacheBytes.Load())
	}
}

func TestCacheStore_EvictsOldestWhenFull(t *testing.T) {
	// 1MB 缓存
	d := newTestDeduplicator(1)

	// 写入 500KB
	data500k := make([]byte, 500*1024)
	d.cacheStore("first", 1, data500k)

	// 再写入 400KB，仍在限制内
	data400k := make([]byte, 400*1024)
	d.cacheStore("second", 2, data400k)

	// 此时总计 900KB < 1MB，两个都应该在
	if _, ok := d.cache.Load("first"); !ok {
		t.Error("expected 'first' to still be in cache")
	}
	if _, ok := d.cache.Load("second"); !ok {
		t.Error("expected 'second' to still be in cache")
	}

	// 再写入 200KB，总计 1100KB > 1MB，应该淘汰最旧的 "first"
	data200k := make([]byte, 200*1024)
	d.cacheStore("third", 3, data200k)

	if _, ok := d.cache.Load("first"); ok {
		t.Error("expected 'first' to be evicted (oldest)")
	}
	if _, ok := d.cache.Load("second"); !ok {
		t.Error("expected 'second' to still be in cache")
	}
	if _, ok := d.cache.Load("third"); !ok {
		t.Error("expected 'third' to still be in cache")
	}
}

func TestCacheStore_EvictsExpiredFirst(t *testing.T) {
	d := newTestDeduplicator(1) // 1MB

	// 手动插入一个过期条目
	expiredEntry := &resourceCacheEntry{
		resourceID: 1,
		data:       make([]byte, 500*1024),
		size:       500 * 1024,
		cachedAt:   time.Now().Add(-3 * time.Hour), // 超过 2 小时 TTL
	}
	d.cache.Store("expired", expiredEntry)
	d.cacheBytes.Add(expiredEntry.size)

	// 插入一个新的未过期条目
	d.cacheStore("fresh", 2, make([]byte, 400*1024))

	// 再插入一个，总共超出限制，应该优先淘汰过期的
	d.cacheStore("newest", 3, make([]byte, 200*1024))

	if _, ok := d.cache.Load("expired"); ok {
		t.Error("expected 'expired' to be evicted first (TTL expired)")
	}
	if _, ok := d.cache.Load("fresh"); !ok {
		t.Error("expected 'fresh' to remain in cache")
	}
	if _, ok := d.cache.Load("newest"); !ok {
		t.Error("expected 'newest' to remain in cache")
	}
}

func TestCacheStore_SkipsOversizedEntry(t *testing.T) {
	d := newTestDeduplicator(1) // 1MB

	// 单个条目超过整个缓存容量
	huge := make([]byte, 2*1024*1024) // 2MB
	d.cacheStore("huge", 1, huge)

	if _, ok := d.cache.Load("huge"); ok {
		t.Error("expected oversized entry to not be cached")
	}
	if d.cacheBytes.Load() != 0 {
		t.Errorf("cacheBytes = %d, want 0 (nothing should be cached)", d.cacheBytes.Load())
	}
}

func TestCacheStore_SizeTrackingAccurate(t *testing.T) {
	d := newTestDeduplicator(100)

	d.cacheStore("a", 1, make([]byte, 100))
	d.cacheStore("b", 2, make([]byte, 200))
	d.cacheStore("c", 3, make([]byte, 300))

	expected := int64(100 + 200 + 300)
	if d.cacheBytes.Load() != expected {
		t.Errorf("cacheBytes = %d, want %d", d.cacheBytes.Load(), expected)
	}

	// 覆盖 b 为更小的数据
	d.cacheStore("b", 2, make([]byte, 50))
	expected = int64(100 + 50 + 300)
	if d.cacheBytes.Load() != expected {
		t.Errorf("cacheBytes = %d, want %d after overwrite", d.cacheBytes.Load(), expected)
	}
}

func TestCacheStore_ConcurrentAccess(t *testing.T) {
	d := newTestDeduplicator(10) // 10MB

	done := make(chan struct{})
	// 并发写入
	for i := 0; i < 100; i++ {
		go func(i int) {
			data := make([]byte, 10*1024) // 10KB each
			d.cacheStore(string(rune('A'+i%26))+string(rune('0'+i/26)), int64(i), data)
			done <- struct{}{}
		}(i)
	}

	for i := 0; i < 100; i++ {
		<-done
	}

	// 不应该 panic，且 cacheBytes 应该 > 0
	if d.cacheBytes.Load() <= 0 {
		t.Errorf("cacheBytes = %d, want > 0 after concurrent writes", d.cacheBytes.Load())
	}

	// cacheBytes 不应该超过限制
	maxBytes := int64(10 * 1024 * 1024)
	if d.cacheBytes.Load() > maxBytes {
		t.Errorf("cacheBytes = %d, exceeds limit %d", d.cacheBytes.Load(), maxBytes)
	}
}

func TestCacheStore_NilDataLargeFile(t *testing.T) {
	d := newTestDeduplicator(100)

	// 大文件流式落盘后 data 为 nil，cacheStore 应该正常处理
	d.cacheStore("large-file-url", 42, nil)

	entry, ok := d.cache.Load("large-file-url")
	if !ok {
		t.Fatal("expected cache entry for large-file-url")
	}

	cached := entry.(*resourceCacheEntry)
	if cached.resourceID != 42 {
		t.Errorf("resourceID = %d, want 42", cached.resourceID)
	}
	if cached.data != nil {
		t.Errorf("data should be nil for large file, got %d bytes", len(cached.data))
	}
	if cached.size != 0 {
		t.Errorf("size = %d, want 0 for nil data", cached.size)
	}

	// cacheBytes 不应增加
	if d.cacheBytes.Load() != 0 {
		t.Errorf("cacheBytes = %d, want 0 (nil data should add 0)", d.cacheBytes.Load())
	}
}

func TestCacheStore_NilDataCacheHitReturnsNil(t *testing.T) {
	d := newTestDeduplicator(100)

	// 模拟大文件缓存 entry（data 为 nil）
	d.cacheStore("css-url", 10, nil)

	// 缓存命中时应返回 nil data 和正确的 resourceID
	entry, ok := d.cache.Load("css-url")
	if !ok {
		t.Fatal("expected cache hit")
	}
	cached := entry.(*resourceCacheEntry)
	if cached.resourceID != 10 {
		t.Errorf("resourceID = %d, want 10", cached.resourceID)
	}
	// 调用方需要自行处理 nil data（例如从磁盘读取 CSS 内容）
	if cached.data != nil {
		t.Error("expected nil data from cache for large file")
	}
}
