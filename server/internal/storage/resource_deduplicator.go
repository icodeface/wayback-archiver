package storage

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"wayback/internal/database"
	"wayback/internal/models"
)

type ResourceProcessRequest struct {
	URL     string
	Type    string
	PageURL string
	Headers map[string]string
	Cookies []models.CaptureCookie
}
type ResourceProcessResult struct {
	ResourceID int64
	FilePath   string
	Data       []byte
}

type ResourceDeduplicator struct {
	db                database.Database
	storage           *FileStorage
	cache             *ResourceCache
	downloader        *ResourceDownloader
	concurrency       *ConcurrencyManager
	streamThresholdKB int
	beforeCreate      func(string)
}

func NewResourceDeduplicator(db database.Database, storage *FileStorage, cache *ResourceCache, downloader *ResourceDownloader, concurrency *ConcurrencyManager) *ResourceDeduplicator {
	return &ResourceDeduplicator{db: db, storage: storage, cache: cache, downloader: downloader, concurrency: concurrency}
}

func (d *ResourceDeduplicator) Process(req ResourceProcessRequest) (ResourceProcessResult, error) {
	if d == nil || d.db == nil || d.storage == nil || d.downloader == nil {
		return ResourceProcessResult{}, fmt.Errorf("resource deduplicator is not configured")
	}
	id, path, data, err := d.processResource(req.URL, req.Type, req.PageURL, req.Headers, req.Cookies)
	return ResourceProcessResult{ResourceID: id, FilePath: path, Data: data}, err
}

func (d *ResourceDeduplicator) ensureReusableResource(resource *models.Resource, reuseURL string) bool {
	if resource == nil {
		return false
	}
	if resource.IsQuarantined {
		log.Printf("[resource] skip quarantined reuse id=%d url=%s reason=%s", resource.ID, shortURLForLog(reuseURL), resource.QuarantineReason)
		return false
	}
	if resource.FilePath == "" {
		log.Printf("[resource] skip reuse for empty file path id=%d url=%s", resource.ID, shortURLForLog(reuseURL))
		return false
	}
	return true
}

func (d *ResourceDeduplicator) tryReuseFreshCache(url string, cached *resourceCacheEntry) (int64, string, bool) {
	if cached == nil || cached.freshUntil.IsZero() || !time.Now().Before(cached.freshUntil) {
		return 0, "", false
	}
	resource, err := d.db.GetResourceByID(cached.resourceID)
	if err != nil || !d.ensureReusableResource(resource, url) {
		d.cache.Delete(url)
		return 0, "", false
	}
	d.cache.Store(url, resource.ID, resource.FilePath, downloadMetadata{etag: cached.etag, lastMod: cached.lastMod, freshUntil: cached.freshUntil})
	log.Printf("[cache] fresh reuse: %s", shortURLForLog(url))
	return resource.ID, resource.FilePath, true
}

func (d *ResourceDeduplicator) processResourceFallback(url string, downloadErr error) (int64, string, []byte, error) {
	resource, err := d.db.GetResourceByURL(url)
	if err != nil {
		return 0, "", nil, fmt.Errorf("download failed: %w (fallback lookup failed: %v)", downloadErr, err)
	}
	if !d.ensureReusableResource(resource, url) {
		return 0, "", nil, fmt.Errorf("download failed: %w", downloadErr)
	}
	if _, err := os.Stat(filepath.Join(d.storage.baseDir, resource.FilePath)); err != nil {
		if os.IsNotExist(err) {
			return 0, "", nil, fmt.Errorf("download failed: %w", downloadErr)
		}
		return 0, "", nil, fmt.Errorf("download failed: %w (fallback stat failed: %v)", downloadErr, err)
	}
	if err := d.db.UpdateResourceLastSeen(resource.ID); err != nil {
		return 0, "", nil, fmt.Errorf("download failed: %w (fallback last_seen update failed: %v)", downloadErr, err)
	}
	d.cache.Store(url, resource.ID, resource.FilePath, downloadMetadata{})
	log.Printf("Fallback: reusing previous resource (ID: %d) for: %s", resource.ID, shortURLForLog(url))
	return resource.ID, resource.FilePath, nil, nil
}

func (d *ResourceDeduplicator) loadCachedResource(url string) *resourceCacheEntry {
	return d.cache.Load(url)
}
func (d *ResourceDeduplicator) cacheDelete(url string) { d.cache.Delete(url) }
func (d *ResourceDeduplicator) cacheStore(url string, resourceID int64, filePath string, _ []byte) {
	d.cache.Store(url, resourceID, filePath, downloadMetadata{})
}
func (d *ResourceDeduplicator) cacheStoreWithMetadata(url string, resourceID int64, filePath string, metadata downloadMetadata, _ []byte) {
	d.cache.Store(url, resourceID, filePath, metadata)
}

func (d *ResourceDeduplicator) processResource(url, resourceType string, pageURL string, headers map[string]string, cookies []models.CaptureCookie) (int64, string, []byte, error) {
	unlock := d.concurrency.LockResource(url)
	defer unlock()
	if d.beforeCreate != nil {
		d.beforeCreate(url)
	}

	startTime := time.Now()
	cached := d.loadCachedResource(url)
	if resourceID, filePath, reused := d.tryReuseFreshCache(url, cached); reused {
		return resourceID, filePath, nil, nil
	}

	var data []byte    // 小文件有值，大文件 nil
	var tmpPath string // 大文件临时文件路径，小文件空
	var hash string
	var fileSize int64
	var metadata downloadMetadata
	var trace downloadTrace
	var dbDuration time.Duration
	var saveDuration time.Duration

	streamThreshold := int64(d.streamThresholdKB) * 1024
	var err error
	downloadResult, err := d.downloader.Download(ResourceDownloadRequest{URL: url, PageURL: pageURL, Headers: headers, Cookies: cookies, StreamThreshold: streamThreshold, ETag: cachedETag(cached), LastModified: cachedLastModified(cached)})
	data, hash, tmpPath = downloadResult.Data, downloadResult.Hash, downloadResult.TempPath
	metadata = downloadMetadata{etag: downloadResult.Metadata.ETag, lastMod: downloadResult.Metadata.LastModified, freshUntil: downloadResult.Metadata.FreshUntil, hasFreshness: downloadResult.Metadata.HasFreshness, notModified: downloadResult.Metadata.NotModified}
	trace = downloadTrace{validate: downloadResult.Trace.Validation, request: downloadResult.Trace.Request, body: downloadResult.Trace.Body, mode: downloadResult.Trace.Mode, statusCode: downloadResult.Trace.StatusCode, contentSize: downloadResult.Trace.ContentSize}
	if err != nil {
		log.Printf("Download failed for %s: %v", url, err)
		return d.processResourceFallback(url, err)
	}
	if metadata.notModified {
		if cached == nil {
			return 0, "", nil, fmt.Errorf("received 304 without cache entry for %s", url)
		}
		if cached.filePath == "" {
			d.cacheDelete(url)
			freshResult, freshErr := d.downloader.Download(ResourceDownloadRequest{URL: url, PageURL: pageURL, Headers: headers, Cookies: cookies, StreamThreshold: streamThreshold})
			data, hash, tmpPath, err = freshResult.Data, freshResult.Hash, freshResult.TempPath, freshErr
			metadata = downloadMetadata{etag: freshResult.Metadata.ETag, lastMod: freshResult.Metadata.LastModified, freshUntil: freshResult.Metadata.FreshUntil, hasFreshness: freshResult.Metadata.HasFreshness, notModified: freshResult.Metadata.NotModified}
			trace = downloadTrace{validate: freshResult.Trace.Validation, request: freshResult.Trace.Request, body: freshResult.Trace.Body, mode: freshResult.Trace.Mode, statusCode: freshResult.Trace.StatusCode, contentSize: freshResult.Trace.ContentSize}
			if err != nil {
				log.Printf("Download failed for %s after cache revalidation miss: %v", url, err)
				return d.processResourceFallback(url, err)
			}
		} else {
			cachedResource, err := d.db.GetResourceByID(cached.resourceID)
			if err != nil {
				return 0, "", nil, fmt.Errorf("db query cached resource failed: %w", err)
			}
			if !d.ensureReusableResource(cachedResource, url) {
				d.cacheDelete(url)
				freshResult, freshErr := d.downloader.Download(ResourceDownloadRequest{URL: url, PageURL: pageURL, Headers: headers, Cookies: cookies, StreamThreshold: streamThreshold})
				data, hash, tmpPath, err = freshResult.Data, freshResult.Hash, freshResult.TempPath, freshErr
				metadata = downloadMetadata{etag: freshResult.Metadata.ETag, lastMod: freshResult.Metadata.LastModified, freshUntil: freshResult.Metadata.FreshUntil, hasFreshness: freshResult.Metadata.HasFreshness, notModified: freshResult.Metadata.NotModified}
				trace = downloadTrace{validate: freshResult.Trace.Validation, request: freshResult.Trace.Request, body: freshResult.Trace.Body, mode: freshResult.Trace.Mode, statusCode: freshResult.Trace.StatusCode, contentSize: freshResult.Trace.ContentSize}
				if err != nil {
					log.Printf("Download failed for %s after corrupted cache revalidation miss: %v", url, err)
					return d.processResourceFallback(url, err)
				}
			} else {
				if !metadata.hasFreshness {
					metadata.freshUntil = cached.freshUntil
				}
				if metadata.etag == "" {
					metadata.etag = cached.etag
				}
				if metadata.lastMod == "" {
					metadata.lastMod = cached.lastMod
				}
				d.cacheStoreWithMetadata(url, cachedResource.ID, cachedResource.FilePath, metadata, nil)
				log.Printf("[cache] revalidated 304: %s (%v)", shortURLForLog(url), time.Since(startTime))
				return cachedResource.ID, cachedResource.FilePath, nil, nil
			}
		}
	}
	if data != nil {
		fileSize = int64(len(data))
	} else if tmpPath != "" {
		if info, statErr := os.Stat(tmpPath); statErr == nil {
			fileSize = info.Size()
		}
	}

	// 确保大文件临时文件最终被清理（SaveResourceFromFile 成功后会置空 tmpPath）
	if tmpPath != "" {
		defer func() {
			if tmpPath != "" {
				os.Remove(tmpPath)
			}
		}()
	}

	if cached != nil {
		dbStart := time.Now()
		cachedResource, err := d.db.GetResourceByID(cached.resourceID)
		dbDuration += time.Since(dbStart)
		if err != nil {
			return 0, "", nil, fmt.Errorf("db query cached resource failed: %w", err)
		}
		if cachedResource != nil && cachedResource.ContentHash == hash && d.ensureReusableResource(cachedResource, url) {
			dbStart = time.Now()
			if err := d.db.UpdateResourceLastSeen(cachedResource.ID); err != nil {
				dbDuration += time.Since(dbStart)
				return 0, "", nil, err
			}
			dbDuration += time.Since(dbStart)
			d.cacheStoreWithMetadata(url, cachedResource.ID, cachedResource.FilePath, metadata, data)
			logSlowResource(url, resourceType, fileSize, trace, dbDuration, saveDuration, time.Since(startTime))
			return cachedResource.ID, cachedResource.FilePath, data, nil
		}
	}

	// 检查是否已有相同 URL 的资源记录
	dbStart := time.Now()
	existingByURL, err := d.db.GetResourceByURL(url)
	dbDuration += time.Since(dbStart)
	if err != nil {
		return 0, "", nil, fmt.Errorf("db query by url failed: %w", err)
	}
	if existingByURL != nil && !d.ensureReusableResource(existingByURL, url) {
		existingByURL = nil
	}
	if existingByURL != nil {
		if existingByURL.ContentHash == hash {
			dbStart = time.Now()
			if err := d.db.UpdateResourceLastSeen(existingByURL.ID); err != nil {
				dbDuration += time.Since(dbStart)
				return 0, "", nil, err
			}
			dbDuration += time.Since(dbStart)
			d.cacheStoreWithMetadata(url, existingByURL.ID, existingByURL.FilePath, metadata, data)
			logSlowResource(url, resourceType, fileSize, trace, dbDuration, saveDuration, time.Since(startTime))
			return existingByURL.ID, existingByURL.FilePath, data, nil
		}

		log.Printf("Resource content changed for URL %s: old_hash=%s new_hash=%s", url, existingByURL.ContentHash[:16], hash[:16])
	}

	// 检查是否有相同哈希的资源
	dbStart = time.Now()
	existingByHash, err := d.db.GetResourceByHash(hash)
	dbDuration += time.Since(dbStart)
	if err != nil {
		return 0, "", nil, fmt.Errorf("db query by hash failed: %w", err)
	}
	if existingByHash != nil && !d.ensureReusableResource(existingByHash, url) {
		existingByHash = nil
	}

	var filePath string
	if existingByHash != nil {
		filePath = existingByHash.FilePath
	} else if tmpPath != "" {
		// 大文件：从临时文件移动到资源目录（零拷贝）
		saveStart := time.Now()
		filePath, err = d.storage.SaveResourceFromFile(tmpPath, hash, resourceType)
		saveDuration += time.Since(saveStart)
		if err != nil {
			return 0, "", nil, fmt.Errorf("save from file failed: %w", err)
		}
		tmpPath = "" // 已被移走，阻止 defer 删除
	} else {
		// 小文件：从内存写入
		saveStart := time.Now()
		filePath, err = d.storage.SaveResource(data, hash, resourceType)
		saveDuration += time.Since(saveStart)
		if err != nil {
			return 0, "", nil, fmt.Errorf("save failed: %w", err)
		}
	}

	dbStart = time.Now()
	resourceID, err := d.db.CreateResource(url, hash, resourceType, filePath, fileSize)
	dbDuration += time.Since(dbStart)
	if err != nil {
		return 0, "", nil, fmt.Errorf("db insert failed: %w", err)
	}

	d.cacheStoreWithMetadata(url, resourceID, filePath, metadata, data)
	logSlowResource(url, resourceType, fileSize, trace, dbDuration, saveDuration, time.Since(startTime))
	return resourceID, filePath, data, nil
}
