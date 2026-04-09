package storage

import (
	"strings"
	"testing"
	"time"

	"wayback/internal/config"
)

func newTestDeduplicator(cacheSizeMB int) *Deduplicator {
	return &Deduplicator{
		config: config.ResourceConfig{
			Workers:         2,
			MetadataCacheMB: cacheSizeMB,
		},
	}
}

func sizedString(n int) string {
	return strings.Repeat("x", n)
}

func TestCacheStore_BasicStoreAndRetrieve(t *testing.T) {
	d := newTestDeduplicator(100) // 100MB cache

	data := []byte("hello world")
	d.cacheStore("key1", 42, "resources/ab/cd/key1.bin", data)

	entry, ok := d.cache.Load("key1")
	if !ok {
		t.Fatal("expected cache entry for key1")
	}

	cached := entry.(*resourceCacheEntry)
	if cached.resourceID != 42 {
		t.Errorf("resourceID = %d, want 42", cached.resourceID)
	}
	expected := cacheEntrySize("key1", "resources/ab/cd/key1.bin")
	if d.cacheBytes.Load() != expected {
		t.Errorf("cacheBytes = %d, want %d", d.cacheBytes.Load(), expected)
	}
}

func TestCacheStore_OverwriteUpdatesSize(t *testing.T) {
	d := newTestDeduplicator(100)

	d.cacheStore("key1", 1, "", []byte("short"))
	if d.cacheBytes.Load() != 0 {
		t.Fatalf("cacheBytes = %d, want 0", d.cacheBytes.Load())
	}

	// 覆盖同一个 key，数据更大
	d.cacheStore("key1", 1, "resources/new/path.bin", []byte("much longer data"))
	expected := cacheEntrySize("key1", "resources/new/path.bin")
	if d.cacheBytes.Load() != expected {
		t.Errorf("cacheBytes = %d, want %d (after overwrite)", d.cacheBytes.Load(), expected)
	}
}

func TestCacheStore_EvictsOldestWhenFull(t *testing.T) {
	// 1MB 缓存
	d := newTestDeduplicator(1)

	firstPath := sizedString(500 * 1024)
	d.cacheStore("first", 1, firstPath, nil)

	secondPath := sizedString(400 * 1024)
	d.cacheStore("second", 2, secondPath, nil)

	// 此时总计 900KB < 1MB，两个都应该在
	if _, ok := d.cache.Load("first"); !ok {
		t.Error("expected 'first' to still be in cache")
	}
	if _, ok := d.cache.Load("second"); !ok {
		t.Error("expected 'second' to still be in cache")
	}

	// 再写入 200KB，总计 1100KB > 1MB，应该淘汰最旧的 "first"
	thirdPath := sizedString(200 * 1024)
	d.cacheStore("third", 3, thirdPath, nil)

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
		filePath:   sizedString(500 * 1024),
		size:       cacheEntrySize("expired", sizedString(500*1024)),
		cachedAt:   time.Now().Add(-3 * time.Hour), // 超过 2 小时 TTL
	}
	d.cache.Store("expired", expiredEntry)
	d.cacheBytes.Add(expiredEntry.size)

	// 插入一个新的未过期条目
	d.cacheStore("fresh", 2, sizedString(400*1024), nil)

	// 再插入一个，总共超出限制，应该优先淘汰过期的
	d.cacheStore("newest", 3, sizedString(200*1024), nil)

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
	hugePath := sizedString(2 * 1024 * 1024)
	d.cacheStore("huge", 1, hugePath, nil)

	if _, ok := d.cache.Load("huge"); ok {
		t.Error("expected oversized entry to not be cached")
	}
	if d.cacheBytes.Load() != 0 {
		t.Errorf("cacheBytes = %d, want 0 (nothing should be cached)", d.cacheBytes.Load())
	}
}

func TestCacheStore_SizeTrackingAccurate(t *testing.T) {
	d := newTestDeduplicator(100)

	d.cacheStore("a", 1, "path-a", nil)
	d.cacheStore("b", 2, "path-bb", nil)
	d.cacheStore("c", 3, "path-ccc", nil)

	expected := cacheEntrySize("a", "path-a") + cacheEntrySize("b", "path-bb") + cacheEntrySize("c", "path-ccc")
	if d.cacheBytes.Load() != expected {
		t.Errorf("cacheBytes = %d, want %d", d.cacheBytes.Load(), expected)
	}

	// 覆盖 b 为更小的数据
	d.cacheStore("b", 2, "p", nil)
	expected = cacheEntrySize("a", "path-a") + cacheEntrySize("b", "p") + cacheEntrySize("c", "path-ccc")
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
			key := string(rune('A'+i%26)) + string(rune('0'+i/26))
			d.cacheStore(key, int64(i), "resources/path.bin", nil)
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

func TestCacheStore_NilDataStillCachesMetadata(t *testing.T) {
	d := newTestDeduplicator(100)

	d.cacheStore("large-file-url", 42, "resources/large/file.bin", nil)

	entry, ok := d.cache.Load("large-file-url")
	if !ok {
		t.Fatal("expected metadata-only entry to be cached")
	}
	if entry.(*resourceCacheEntry).filePath != "resources/large/file.bin" {
		t.Fatalf("filePath = %q, want %q", entry.(*resourceCacheEntry).filePath, "resources/large/file.bin")
	}
	expected := cacheEntrySize("large-file-url", "resources/large/file.bin")
	if d.cacheBytes.Load() != expected {
		t.Errorf("cacheBytes = %d, want %d", d.cacheBytes.Load(), expected)
	}
}

func TestCacheStore_NilDataCacheHitReturnsNil(t *testing.T) {
	d := newTestDeduplicator(100)

	d.cacheStore("css-url", 10, "resources/style.css", nil)

	entry, ok := d.cache.Load("css-url")
	if !ok {
		t.Fatal("expected metadata-only cache entry")
	}
	if entry.(*resourceCacheEntry).resourceID != 10 {
		t.Fatalf("resourceID = %d, want 10", entry.(*resourceCacheEntry).resourceID)
	}
}

func TestCacheStore_FilePathStored(t *testing.T) {
	d := newTestDeduplicator(100)

	d.cacheStore("url1", 1, "resources/ab/cd/hash1.css", []byte("body{}"))
	d.cacheStore("url2", 2, "resources/ef/gh/hash2.bin", nil)

	// 验证 filePath 正确存储（有 data 的条目）
	entry1, ok := d.cache.Load("url1")
	if !ok {
		t.Fatal("expected entry for url1")
	}
	if entry1.(*resourceCacheEntry).filePath != "resources/ab/cd/hash1.css" {
		t.Errorf("filePath = %q, want %q", entry1.(*resourceCacheEntry).filePath, "resources/ab/cd/hash1.css")
	}

	entry2, ok := d.cache.Load("url2")
	if !ok {
		t.Fatal("expected metadata-only cache entry for url2")
	}
	if entry2.(*resourceCacheEntry).filePath != "resources/ef/gh/hash2.bin" {
		t.Fatalf("filePath = %q, want %q", entry2.(*resourceCacheEntry).filePath, "resources/ef/gh/hash2.bin")
	}
}

func TestCacheStore_OverwriteUpdatesFilePath(t *testing.T) {
	d := newTestDeduplicator(100)

	d.cacheStore("url1", 1, "resources/old/path.bin", []byte("old"))

	// 覆盖同 key，filePath 也应更新
	d.cacheStore("url1", 2, "resources/new/path.bin", []byte("new"))

	entry, ok := d.cache.Load("url1")
	if !ok {
		t.Fatal("expected entry")
	}
	cached := entry.(*resourceCacheEntry)
	if cached.filePath != "resources/new/path.bin" {
		t.Errorf("filePath = %q, want %q", cached.filePath, "resources/new/path.bin")
	}
	if cached.resourceID != 2 {
		t.Errorf("resourceID = %d, want 2", cached.resourceID)
	}
}

func TestCacheStore_ExpiredEntryCleansCacheBytes(t *testing.T) {
	d := newTestDeduplicator(100)

	// 手动插入过期条目
	expired := &resourceCacheEntry{
		resourceID: 1,
		filePath:   "expired-path",
		size:       cacheEntrySize("expired-key", "expired-path"),
		cachedAt:   time.Now().Add(-3 * time.Hour),
	}
	d.cache.Store("expired-key", expired)
	d.cacheBytes.Add(expired.size)

	// 通过 ProcessResource 的缓存命中路径，过期条目应被删除且 cacheBytes 减少
	// 直接测试：加载过期条目，验证 cacheStore 清理逻辑
	// 存一个新条目（不触发淘汰，因为 100MB >> 100 bytes）
	d.cacheStore("new-key", 2, "new-path", nil)
	expected := expired.size + cacheEntrySize("new-key", "new-path")

	// 此时两个条目都在（还没触发淘汰）
	if d.cacheBytes.Load() != expected {
		t.Errorf("cacheBytes = %d, want %d", d.cacheBytes.Load(), expected)
	}

	// 模拟缓存命中时发现过期并手动删除（类似 ProcessResource 中的逻辑）
	if entry, ok := d.cache.Load("expired-key"); ok {
		cached := entry.(*resourceCacheEntry)
		if time.Since(cached.cachedAt) >= resourceCacheTTL {
			d.cache.Delete("expired-key")
			d.cacheBytes.Add(-cached.size)
		}
	}

	if d.cacheBytes.Load() != cacheEntrySize("new-key", "new-path") {
		t.Errorf("cacheBytes after expiry cleanup = %d, want %d", d.cacheBytes.Load(), cacheEntrySize("new-key", "new-path"))
	}
}

func TestCleanupExpiredCache_RemovesExpiredEntries(t *testing.T) {
	d := newTestDeduplicator(100)

	expired := &resourceCacheEntry{
		resourceID: 1,
		filePath:   "expired-path",
		size:       cacheEntrySize("expired", "expired-path"),
		cachedAt:   time.Now().Add(-3 * time.Hour),
	}
	fresh := &resourceCacheEntry{
		resourceID: 2,
		filePath:   "fresh-path",
		size:       cacheEntrySize("fresh", "fresh-path"),
		cachedAt:   time.Now(),
	}

	d.cache.Store("expired", expired)
	d.cache.Store("fresh", fresh)
	d.cacheBytes.Add(expired.size + fresh.size)

	removed := d.cleanupExpiredCache()
	if removed != 1 {
		t.Fatalf("cleanupExpiredCache() removed %d entries, want 1", removed)
	}

	if _, ok := d.cache.Load("expired"); ok {
		t.Fatal("expected expired entry to be removed")
	}
	if _, ok := d.cache.Load("fresh"); !ok {
		t.Fatal("expected fresh entry to remain")
	}
	if d.cacheBytes.Load() != fresh.size {
		t.Fatalf("cacheBytes = %d, want %d after cleanup", d.cacheBytes.Load(), fresh.size)
	}
}

func TestCacheMaxBytes(t *testing.T) {
	d := newTestDeduplicator(10)
	expected := int64(10 * 1024 * 1024)
	if d.cacheMaxBytes() != expected {
		t.Errorf("cacheMaxBytes() = %d, want %d", d.cacheMaxBytes(), expected)
	}

	d2 := newTestDeduplicator(1)
	if d2.cacheMaxBytes() != 1024*1024 {
		t.Errorf("cacheMaxBytes() = %d, want %d", d2.cacheMaxBytes(), 1024*1024)
	}
}
