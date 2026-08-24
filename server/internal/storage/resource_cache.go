package storage

import (
	"sync"
	"sync/atomic"
	"time"
)

// ResourceCache 管理资源 URL 的 freshness/validator 元数据。
type ResourceCache struct {
	entries  *sync.Map
	maxBytes int64
	cacheMu  *sync.Mutex
	bytes    *atomic.Int64
	stopOnce sync.Once
	stop     chan struct{}
	interval time.Duration
	ttl      time.Duration
}

type CachedResource struct {
	ResourceID   int64
	FilePath     string
	ETag         string
	LastModified string
	FreshUntil   time.Time
}

func NewResourceCache(metadataCacheMB int) *ResourceCache {
	return newResourceCacheWithBacking(metadataCacheMB, &sync.Map{}, &sync.Mutex{}, &atomic.Int64{})
}

func newResourceCacheWithBacking(metadataCacheMB int, entries *sync.Map, cacheMu *sync.Mutex, bytes *atomic.Int64) *ResourceCache {
	return &ResourceCache{entries: entries, maxBytes: int64(metadataCacheMB) * 1024 * 1024, cacheMu: cacheMu, bytes: bytes, stop: make(chan struct{}), interval: resourceCacheCleanupInterval, ttl: resourceCacheTTL}
}

func (c *ResourceCache) MaxBytes() int64 {
	if c == nil {
		return 0
	}
	return c.maxBytes
}
func (c *ResourceCache) Bytes() int64 {
	if c == nil || c.bytes == nil {
		return 0
	}
	return c.bytes.Load()
}

func (c *ResourceCache) Get(key string) (CachedResource, bool) {
	entry := c.Load(key)
	if entry == nil {
		return CachedResource{}, false
	}
	return CachedResource{ResourceID: entry.resourceID, FilePath: entry.filePath, ETag: entry.etag, LastModified: entry.lastMod, FreshUntil: entry.freshUntil}, true
}

func (c *ResourceCache) Put(key string, value CachedResource) {
	c.Store(key, value.ResourceID, value.FilePath, downloadMetadata{etag: value.ETag, lastMod: value.LastModified, freshUntil: value.FreshUntil})
}

func (c *ResourceCache) Load(key string) *resourceCacheEntry {
	if c == nil {
		return nil
	}
	v, ok := c.entries.Load(key)
	if !ok {
		return nil
	}
	entry := v.(*resourceCacheEntry)
	if time.Since(entry.cachedAt) >= c.ttl {
		c.Delete(key)
		return nil
	}
	return entry
}

func (c *ResourceCache) Delete(key string) {
	if c == nil {
		return
	}
	if old, ok := c.entries.LoadAndDelete(key); ok {
		c.bytes.Add(-old.(*resourceCacheEntry).size)
	}
}

func (c *ResourceCache) Store(key string, resourceID int64, filePath string, metadata downloadMetadata) {
	if c == nil || key == "" || filePath == "" || c.maxBytes <= 0 {
		return
	}
	size := cacheEntrySize(key, filePath)
	if size > c.maxBytes {
		return
	}
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	if old, ok := c.entries.LoadAndDelete(key); ok {
		c.bytes.Add(-old.(*resourceCacheEntry).size)
	}
	for c.bytes.Load()+size > c.maxBytes {
		var oldestKey any
		var oldest time.Time
		evictedExpired := false
		c.entries.Range(func(k, v any) bool {
			entry := v.(*resourceCacheEntry)
			if time.Since(entry.cachedAt) >= c.ttl {
				if old, ok := c.entries.LoadAndDelete(k); ok {
					c.bytes.Add(-old.(*resourceCacheEntry).size)
					evictedExpired = true
				}
				return false
			}
			if oldestKey == nil || entry.cachedAt.Before(oldest) {
				oldestKey, oldest = k, entry.cachedAt
			}
			return true
		})
		if evictedExpired {
			continue
		}
		if oldestKey == nil {
			break
		}
		if old, ok := c.entries.LoadAndDelete(oldestKey); ok {
			c.bytes.Add(-old.(*resourceCacheEntry).size)
		}
	}
	c.entries.Store(key, &resourceCacheEntry{resourceID: resourceID, filePath: filePath, etag: metadata.etag, lastMod: metadata.lastMod, freshUntil: metadata.freshUntil, size: size, cachedAt: time.Now()})
	c.bytes.Add(size)
}

func (c *ResourceCache) CleanupExpired() int {
	if c == nil {
		return 0
	}
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	removed := 0
	c.entries.Range(func(k, v any) bool {
		if time.Since(v.(*resourceCacheEntry).cachedAt) >= c.ttl {
			if old, ok := c.entries.LoadAndDelete(k); ok {
				c.bytes.Add(-old.(*resourceCacheEntry).size)
				removed++
			}
		}
		return true
	})
	return removed
}

func (c *ResourceCache) StartCleanup() {
	if c == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(c.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				c.CleanupExpired()
			case <-c.stop:
				return
			}
		}
	}()
}

func (c *ResourceCache) Close() {
	if c != nil {
		c.stopOnce.Do(func() { close(c.stop) })
	}
}
