package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"wayback/internal/config"
	"wayback/internal/database"
	"wayback/internal/models"
)

const (
	resourceCacheTTL             = 2 * time.Hour
	resourceCacheCleanupInterval = 10 * time.Minute
	resourceCacheEntryOverhead   = 128
	slowResourceLogThreshold     = 500 * time.Millisecond
)

type resourceCacheEntry struct {
	resourceID int64
	filePath   string
	etag       string
	lastMod    string
	freshUntil time.Time
	size       int64 // 估算的元数据大小，用于统计缓存大小
	cachedAt   time.Time
}

type Deduplicator struct {
	db            *database.DB
	storage       *FileStorage
	cssParser     *CSSParser
	htmlExtractor *HTMLResourceExtractor
	cache         sync.Map // url -> *resourceCacheEntry
	deletionQueue *DeletionQueue
	config        config.ResourceConfig
	cacheBytes    atomic.Int64  // 当前缓存占用字节数
	cacheMu       sync.Mutex    // 保护缓存淘汰逻辑，防止并发 cacheStore 导致超限
	globalSem     chan struct{} // 全局并发下载信号量，跨所有页面共享
	pageTaskMu    sync.Mutex
	pageTaskSeq   map[int64]uint64
	bgTasks       sync.WaitGroup

	// 测试钩子：用于稳定复现页面已创建/新 HTML 已写入后的失败路径。
	testBeforeCreateFinalize func(pageID int64, htmlPath string, resourceIDs []int64) error
	testBeforeUpdateCommit   func(pageID int64, htmlPath string, resourceIDs []int64) error
}

func NewDeduplicator(db *database.DB, storage *FileStorage, cfg config.ResourceConfig) *Deduplicator {
	d := &Deduplicator{
		db:            db,
		storage:       storage,
		cssParser:     NewCSSParser(),
		htmlExtractor: NewHTMLResourceExtractor(),
		deletionQueue: NewDeletionQueue(storage.baseDir),
		config:        cfg,
		globalSem:     make(chan struct{}, cfg.Workers),
		pageTaskSeq:   make(map[int64]uint64),
	}
	d.startCacheCleanupLoop()
	return d
}

var errStalePageTask = errors.New("stale page task")

func cloneCaptureRequest(req *models.CaptureRequest) *models.CaptureRequest {
	if req == nil {
		return nil
	}

	cloned := &models.CaptureRequest{
		URL:   req.URL,
		Title: req.Title,
		HTML:  req.HTML,
	}
	if len(req.Frames) > 0 {
		cloned.Frames = append([]models.FrameCapture(nil), req.Frames...)
	}
	if len(req.Headers) > 0 {
		cloned.Headers = make(map[string]string, len(req.Headers))
		for k, v := range req.Headers {
			cloned.Headers[k] = v
		}
	}
	return cloned
}

func (d *Deduplicator) nextPageTaskSeq(pageID int64) uint64 {
	d.pageTaskMu.Lock()
	defer d.pageTaskMu.Unlock()
	d.pageTaskSeq[pageID] += 1
	return d.pageTaskSeq[pageID]
}

func (d *Deduplicator) isLatestPageTask(pageID int64, seq uint64) bool {
	d.pageTaskMu.Lock()
	defer d.pageTaskMu.Unlock()
	return d.pageTaskSeq[pageID] == seq
}

func (d *Deduplicator) finishPageTask(pageID int64, seq uint64) {
	d.pageTaskMu.Lock()
	defer d.pageTaskMu.Unlock()
	if d.pageTaskSeq[pageID] == seq {
		delete(d.pageTaskSeq, pageID)
	}
}

func (d *Deduplicator) runPageTask(pageID int64, seq uint64, label string, fn func() error) {
	d.bgTasks.Add(1)
	go func() {
		defer d.bgTasks.Done()
		defer d.finishPageTask(pageID, seq)
		if err := fn(); err != nil {
			if errors.Is(err, errStalePageTask) {
				log.Printf("[%s] Skipped stale background task for page %d", label, pageID)
				return
			}
			log.Printf("[%s] Background task failed for page %d: %v", label, pageID, err)
		}
	}()
}

func (d *Deduplicator) WaitForBackgroundTasks() {
	d.bgTasks.Wait()
}

func (d *Deduplicator) ProcessCaptureAsync(req *models.CaptureRequest) (int64, string, error) {
	capturedAt := time.Now()
	contentHash := hashCaptureContent(req.HTML, req.Frames)

	existingPage, err := d.db.GetPageByURLAndHash(req.URL, contentHash)
	if err != nil {
		return 0, "", fmt.Errorf("check existing page failed: %w", err)
	}
	if existingPage != nil {
		log.Printf("Page content unchanged, updating last visited: %s (ID: %d)", req.URL, existingPage.ID)
		if err := d.db.UpdatePageLastVisited(existingPage.ID, capturedAt); err != nil {
			return 0, "", fmt.Errorf("update last visited failed: %w", err)
		}
		return existingPage.ID, models.ArchiveActionUnchanged, nil
	}

	tempHTMLPath, err := d.storage.SaveHTML(req.URL, req.HTML, capturedAt)
	if err != nil {
		return 0, "", fmt.Errorf("save temp html failed: %w", err)
	}

	pageID, err := d.db.CreatePage(req.URL, req.Title, tempHTMLPath, contentHash, capturedAt)
	if err != nil {
		if deleteErr := d.storage.DeleteHTML(tempHTMLPath); deleteErr != nil {
			log.Printf("Failed to delete temporary HTML %s after create page error: %v", tempHTMLPath, deleteErr)
		}
		return 0, "", fmt.Errorf("create page failed: %w", err)
	}

	bodyText := ExtractBodyText(req.HTML)
	if bodyText != "" {
		if err := d.db.UpdatePageBodyText(pageID, bodyText); err != nil {
			log.Printf("Failed to save body text for page %d: %v", pageID, err)
		}
	}

	seq := d.nextPageTaskSeq(pageID)
	clonedReq := cloneCaptureRequest(req)
	d.runPageTask(pageID, seq, "Create", func() error {
		staleCheck := func() bool { return !d.isLatestPageTask(pageID, seq) }
		return d.finalizeCreateCapture(pageID, tempHTMLPath, capturedAt, clonedReq, staleCheck)
	})

	log.Printf("Page created (ID: %d, hash: %s): %s", pageID, contentHash[:16], req.URL)
	return pageID, models.ArchiveActionCreated, nil
}

func (d *Deduplicator) UpdateCaptureAsync(pageID int64, req *models.CaptureRequest) (string, error) {
	page, err := d.db.GetPageByID(fmt.Sprintf("%d", pageID))
	if err != nil || page == nil {
		return "", fmt.Errorf("page not found: %d", pageID)
	}

	newContentHash := hashCaptureContent(req.HTML, req.Frames)
	if newContentHash == page.ContentHash {
		if err := d.db.UpdatePageLastVisited(pageID, time.Now()); err != nil {
			return "", err
		}
		return models.ArchiveActionUnchanged, nil
	}

	seq := d.nextPageTaskSeq(pageID)
	clonedReq := cloneCaptureRequest(req)
	d.runPageTask(pageID, seq, "Update", func() error {
		staleCheck := func() bool { return !d.isLatestPageTask(pageID, seq) }
		_, err := d.updateCapture(pageID, clonedReq, staleCheck)
		return err
	})

	return models.ArchiveActionUpdated, nil
}

func (d *Deduplicator) startCacheCleanupLoop() {
	ticker := time.NewTicker(resourceCacheCleanupInterval)
	go func() {
		for range ticker.C {
			d.cleanupExpiredCache()
		}
	}()
}

func (d *Deduplicator) cleanupExpiredCache() int {
	d.cacheMu.Lock()
	defer d.cacheMu.Unlock()

	removed := 0
	d.cache.Range(func(k, v any) bool {
		entry := v.(*resourceCacheEntry)
		if time.Since(entry.cachedAt) < resourceCacheTTL {
			return true
		}

		if old, loaded := d.cache.LoadAndDelete(k); loaded {
			d.cacheBytes.Add(-old.(*resourceCacheEntry).size)
			removed++
		}
		return true
	})

	if removed > 0 {
		log.Printf("[cache] evicted %d expired resource entries", removed)
	}

	return removed
}

func (d *Deduplicator) cacheDelete(key string) {
	if old, loaded := d.cache.LoadAndDelete(key); loaded {
		d.cacheBytes.Add(-old.(*resourceCacheEntry).size)
	}
}

func (d *Deduplicator) loadCachedResource(url string) *resourceCacheEntry {
	entry, ok := d.cache.Load(url)
	if !ok {
		return nil
	}

	cached := entry.(*resourceCacheEntry)
	if time.Since(cached.cachedAt) >= resourceCacheTTL {
		d.cacheDelete(url)
		return nil
	}

	return cached
}

func (d *Deduplicator) tryReuseFreshCache(url string, cached *resourceCacheEntry) (int64, string, bool, error) {
	if cached == nil || cached.freshUntil.IsZero() || !time.Now().Before(cached.freshUntil) {
		return 0, "", false, nil
	}

	resource, err := d.db.GetResourceByID(cached.resourceID)
	if err != nil {
		return 0, "", false, fmt.Errorf("db query hot cached resource failed: %w", err)
	}
	if resource == nil || resource.FilePath == "" {
		d.cacheDelete(url)
		return 0, "", false, nil
	}

	if err := d.db.UpdateResourceLastSeen(resource.ID); err != nil {
		return 0, "", false, err
	}

	d.cacheStoreWithMetadata(url, resource.ID, resource.FilePath, downloadMetadata{
		etag:       cached.etag,
		lastMod:    cached.lastMod,
		freshUntil: cached.freshUntil,
	}, nil)
	log.Printf("[cache] fresh reuse: %s", shortURLForLog(url))
	return resource.ID, resource.FilePath, true, nil
}

func shortURLForLog(raw string) string {
	const maxLen = 160
	if len(raw) <= maxLen {
		return raw
	}
	return raw[:maxLen-3] + "..."
}

func logSlowResource(url, resourceType string, fileSize int64, trace downloadTrace, dbDuration, saveDuration, total time.Duration) {
	if total < slowResourceLogThreshold {
		return
	}

	sizeLabel := "unknown"
	if fileSize >= 1024*1024 {
		sizeLabel = fmt.Sprintf("%.1fMB", float64(fileSize)/(1024*1024))
	} else if fileSize >= 1024 {
		sizeLabel = fmt.Sprintf("%.1fKB", float64(fileSize)/1024)
	} else if fileSize >= 0 {
		sizeLabel = fmt.Sprintf("%dB", fileSize)
	}

	log.Printf("[resource] slow total=%v type=%s mode=%s size=%s validate=%v request=%v body=%v db=%v save=%v url=%s",
		total,
		resourceType,
		trace.mode,
		sizeLabel,
		trace.validate,
		trace.request,
		trace.body,
		dbDuration,
		saveDuration,
		shortURLForLog(url),
	)
}

// cacheMaxBytes 返回缓存大小上限（字节）
func (d *Deduplicator) cacheMaxBytes() int64 {
	return int64(d.config.MetadataCacheMB) * 1024 * 1024
}

func cacheEntrySize(key, filePath string) int64 {
	return int64(len(key) + len(filePath) + resourceCacheEntryOverhead)
}

// cacheStore 缓存资源元数据，超出大小限制时淘汰最旧的条目
func (d *Deduplicator) cacheStore(key string, resourceID int64, filePath string, data []byte) {
	d.cacheStoreWithMetadata(key, resourceID, filePath, downloadMetadata{}, data)
}

func (d *Deduplicator) cacheStoreWithMetadata(key string, resourceID int64, filePath string, metadata downloadMetadata, data []byte) {
	_ = data // 资源内容不再缓存，只保留元数据
	if key == "" || filePath == "" {
		return
	}

	entrySize := cacheEntrySize(key, filePath)

	// 如果单个条目就超过缓存上限，不缓存
	if entrySize > d.cacheMaxBytes() {
		return
	}

	// 加锁保护淘汰逻辑，防止并发 cacheStore 导致缓存大小超限
	d.cacheMu.Lock()
	defer d.cacheMu.Unlock()

	// 如果 key 已存在，先减去旧条目大小
	if old, loaded := d.cache.Load(key); loaded {
		d.cacheBytes.Add(-old.(*resourceCacheEntry).size)
	}

	// 淘汰过期和超量条目
	for d.cacheBytes.Load()+entrySize > d.cacheMaxBytes() {
		evicted := false
		var oldestKey any
		var oldestTime time.Time

		d.cache.Range(func(k, v any) bool {
			entry := v.(*resourceCacheEntry)
			// 优先淘汰过期的
			if time.Since(entry.cachedAt) >= resourceCacheTTL {
				if old, loaded := d.cache.LoadAndDelete(k); loaded {
					d.cacheBytes.Add(-old.(*resourceCacheEntry).size)
					evicted = true
					return false // 淘汰一个后重新检查
				}
			}
			// 记录最旧的
			if oldestKey == nil || entry.cachedAt.Before(oldestTime) {
				oldestKey = k
				oldestTime = entry.cachedAt
			}
			return true
		})

		if !evicted {
			// 没有过期的可淘汰，淘汰最旧的
			if oldestKey != nil {
				if old, loaded := d.cache.LoadAndDelete(oldestKey); loaded {
					d.cacheBytes.Add(-old.(*resourceCacheEntry).size)
				}
			} else {
				break // 缓存为空但仍然超限，不应该发生
			}
		}
	}

	entry := &resourceCacheEntry{
		resourceID: resourceID,
		filePath:   filePath,
		etag:       metadata.etag,
		lastMod:    metadata.lastMod,
		freshUntil: metadata.freshUntil,
		size:       entrySize,
		cachedAt:   time.Now(),
	}
	d.cache.Store(key, entry)
	d.cacheBytes.Add(entrySize)
}

// ProcessResource 处理单个资源：下载、去重、存储
// 返回 (resourceID, filePath, data, error)
// 小文件（≤ streamThreshold）保留在内存供当前调用链使用；缓存只保留元数据
func (d *Deduplicator) ProcessResource(url, resourceType string, pageURL string, headers map[string]string) (int64, string, []byte, error) {
	startTime := time.Now()
	cached := d.loadCachedResource(url)
	if resourceID, filePath, reused, err := d.tryReuseFreshCache(url, cached); err != nil {
		return 0, "", nil, err
	} else if reused {
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

	streamThreshold := int64(d.config.StreamThresholdKB) * 1024
	var err error
	data, hash, tmpPath, metadata, trace, err = d.storage.DownloadResourceWithMetadata(
		url,
		pageURL,
		headers,
		streamThreshold,
		cachedETag(cached),
		cachedLastModified(cached),
	)
	if err != nil {
		log.Printf("Download failed for %s: %v", url, err)
		return d.processResourceFallback(url, err)
	}
	if metadata.notModified {
		if cached == nil {
			return 0, "", nil, fmt.Errorf("received 304 without cache entry for %s", url)
		}

		dbStart := time.Now()
		resource, err := d.db.GetResourceByID(cached.resourceID)
		dbDuration += time.Since(dbStart)
		if err != nil {
			return 0, "", nil, fmt.Errorf("db query revalidated resource failed: %w", err)
		}
		if resource == nil || resource.FilePath == "" {
			d.cacheDelete(url)
			data, hash, tmpPath, metadata, trace, err = d.storage.DownloadResourceWithMetadata(url, pageURL, headers, streamThreshold, "", "")
			if err != nil {
				log.Printf("Download failed for %s after cache revalidation miss: %v", url, err)
				return d.processResourceFallback(url, err)
			}
		} else {
			dbStart = time.Now()
			if err := d.db.UpdateResourceLastSeen(resource.ID); err != nil {
				dbDuration += time.Since(dbStart)
				return 0, "", nil, err
			}
			dbDuration += time.Since(dbStart)
			if !metadata.hasFreshness {
				metadata.freshUntil = cached.freshUntil
			}
			if metadata.etag == "" {
				metadata.etag = cached.etag
			}
			if metadata.lastMod == "" {
				metadata.lastMod = cached.lastMod
			}
			d.cacheStoreWithMetadata(url, resource.ID, resource.FilePath, metadata, nil)
			log.Printf("[cache] revalidated 304: %s (%v)", shortURLForLog(url), time.Since(startTime))
			return resource.ID, resource.FilePath, nil, nil
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
		if cachedResource != nil && cachedResource.ContentHash == hash {
			dbStart = time.Now()
			if err := d.db.UpdateResourceLastSeen(cached.resourceID); err != nil {
				dbDuration += time.Since(dbStart)
				return 0, "", nil, err
			}
			dbDuration += time.Since(dbStart)
			d.cacheStoreWithMetadata(url, cached.resourceID, cached.filePath, metadata, data)
			logSlowResource(url, resourceType, fileSize, trace, dbDuration, saveDuration, time.Since(startTime))
			return cached.resourceID, cached.filePath, data, nil
		}
	}

	// 检查是否已有相同 URL 的资源记录
	dbStart := time.Now()
	existingByURL, err := d.db.GetResourceByURL(url)
	dbDuration += time.Since(dbStart)
	if err != nil {
		return 0, "", nil, fmt.Errorf("db query by url failed: %w", err)
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
	resourceID, err := d.db.CreateResourceIfNotExists(url, hash, resourceType, filePath, fileSize)
	dbDuration += time.Since(dbStart)
	if err != nil {
		return 0, "", nil, fmt.Errorf("db insert failed: %w", err)
	}

	d.cacheStoreWithMetadata(url, resourceID, filePath, metadata, data)
	logSlowResource(url, resourceType, fileSize, trace, dbDuration, saveDuration, time.Since(startTime))
	return resourceID, filePath, data, nil
}

func cachedETag(cached *resourceCacheEntry) string {
	if cached == nil {
		return ""
	}
	return cached.etag
}

func cachedLastModified(cached *resourceCacheEntry) string {
	if cached == nil {
		return ""
	}
	return cached.lastMod
}

// processResourceFallback 下载失败时的兜底逻辑
// 下载失败时直接返回错误，避免静默复用旧版本资源导致错误归档。
func (d *Deduplicator) processResourceFallback(url string, downloadErr error) (int64, string, []byte, error) {
	return 0, "", nil, fmt.Errorf("download failed: %w", downloadErr)
}

type cssWorkItem struct {
	cssContent string
	cssURL     string
}

type processedInlineHTML struct {
	resourceID int64
	filePath   string
}

var (
	iframeTagMatchRe     = regexp.MustCompile(`(?is)<iframe\b[^>]*>`)
	iframeFrameKeyAttrRe = regexp.MustCompile(`(?i)\sdata-wayback-frame-key=["']([^"']+)["']`)
	iframeSrcAttrMatchRe = regexp.MustCompile(`(?i)(\ssrc=)(["'])([^"']*)(["'])`)
)

func buildFrameCaptureMap(frames []models.FrameCapture) map[string]models.FrameCapture {
	frameMap := make(map[string]models.FrameCapture, len(frames))
	for _, frame := range frames {
		if frame.Key == "" || frame.URL == "" || frame.HTML == "" {
			continue
		}
		// 浏览器侧在 HTML 中通过 data-wayback-frame-key 标记 frame，服务端必须按 key 命中。
		frameMap[frame.Key] = frame
	}
	return frameMap
}

func buildFrameURLSet(frameMap map[string]models.FrameCapture) map[string]struct{} {
	frameURLs := make(map[string]struct{}, len(frameMap))
	for _, frame := range frameMap {
		frameURLs[frame.URL] = struct{}{}
	}
	return frameURLs
}

func hashCaptureContent(html string, frames []models.FrameCapture) string {
	hasher := sha256.New()
	hasher.Write([]byte(html))

	if len(frames) > 0 {
		sorted := append([]models.FrameCapture(nil), frames...)
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].Key < sorted[j].Key
		})
		for _, frame := range sorted {
			hasher.Write([]byte("\n--frame-key--\n"))
			hasher.Write([]byte(frame.Key))
			hasher.Write([]byte("\n--frame-url--\n"))
			hasher.Write([]byte(frame.URL))
			hasher.Write([]byte("\n--frame-title--\n"))
			hasher.Write([]byte(frame.Title))
			hasher.Write([]byte("\n--frame-html--\n"))
			hasher.Write([]byte(frame.HTML))
		}
	}

	return hex.EncodeToString(hasher.Sum(nil))
}

func archiveProxyURL(pageID int64, timestamp, originalURL string) string {
	return fmt.Sprintf("/archive/%d/%smp_/%s", pageID, timestamp, originalURL)
}

func (d *Deduplicator) rewriteIframeTagsByKey(htmlContent string, pageID int64, timestamp string, headers map[string]string, frameMap map[string]models.FrameCapture, resourceIDs *[]int64, seen map[int64]struct{}, visiting map[string]bool, archived map[string]processedInlineHTML) string {
	if len(frameMap) == 0 {
		return htmlContent
	}

	return iframeTagMatchRe.ReplaceAllStringFunc(htmlContent, func(tag string) string {
		keyMatch := iframeFrameKeyAttrRe.FindStringSubmatch(tag)
		if len(keyMatch) < 2 {
			return tag
		}
		frame, ok := frameMap[keyMatch[1]]
		if !ok {
			return tag
		}

		resourceID, _, err := d.archiveFrameCapture(frame, headers, pageID, timestamp, frameMap, resourceIDs, seen, visiting, archived)
		if err != nil {
			log.Printf("Failed to process iframe capture %s: %v", frame.URL, err)
			return tag
		}
		appendUniqueResourceID(resourceIDs, seen, resourceID)

		proxyURL := archiveProxyURL(pageID, timestamp, frame.URL)
		if iframeSrcAttrMatchRe.MatchString(tag) {
			return iframeSrcAttrMatchRe.ReplaceAllString(tag, `${1}${2}`+proxyURL+`${4}`)
		}
		if strings.HasSuffix(tag, "/>") {
			return strings.TrimSuffix(tag, "/>") + ` src="` + proxyURL + `"/>`
		}
		return strings.TrimSuffix(tag, ">") + ` src="` + proxyURL + `">`
	})
}

func appendUniqueResourceID(resourceIDs *[]int64, seen map[int64]struct{}, resourceID int64) {
	if resourceID == 0 {
		return
	}
	if _, ok := seen[resourceID]; ok {
		return
	}
	seen[resourceID] = struct{}{}
	*resourceIDs = append(*resourceIDs, resourceID)
}

func (d *Deduplicator) processInlineResource(url, resourceType string, data []byte) (int64, string, []byte, error) {
	hashBytes := sha256.Sum256(data)
	hash := hex.EncodeToString(hashBytes[:])
	fileSize := int64(len(data))

	existingByHash, err := d.db.GetResourceByHash(hash)
	if err != nil {
		return 0, "", nil, fmt.Errorf("db query by hash failed: %w", err)
	}

	var filePath string
	if existingByHash != nil {
		filePath = existingByHash.FilePath
	} else {
		filePath, err = d.storage.SaveResource(data, hash, resourceType)
		if err != nil {
			return 0, "", nil, fmt.Errorf("save failed: %w", err)
		}
	}

	resourceID, err := d.db.CreateResourceIfNotExists(url, hash, resourceType, filePath, fileSize)
	if err != nil {
		return 0, "", nil, fmt.Errorf("db insert failed: %w", err)
	}

	d.cacheStore(url, resourceID, filePath, data)
	return resourceID, filePath, data, nil
}

func (d *Deduplicator) processCSSWorkItems(cssWorkItems []cssWorkItem, pageURL string, headers map[string]string, rewriter *URLRewriter, resourceIDs *[]int64, seen map[int64]struct{}) {
	type cssSubResource struct {
		absoluteURL string
	}
	var allCSSSubResources []cssSubResource

	for _, cw := range cssWorkItems {
		cssResources := d.cssParser.ExtractResources(cw.cssContent)
		for _, cssResURL := range cssResources {
			absoluteURL := d.resolveURL(cw.cssURL, cssResURL)
			allCSSSubResources = append(allCSSSubResources, cssSubResource{absoluteURL: absoluteURL})
		}
	}

	if len(allCSSSubResources) == 0 {
		return
	}

	type cssSubResult struct {
		sub      cssSubResource
		resID    int64
		filePath string
		err      error
	}

	resultsCh := make(chan cssSubResult, len(allCSSSubResources))
	var wg sync.WaitGroup
	for _, sub := range allCSSSubResources {
		wg.Add(1)
		go func(sub cssSubResource) {
			defer wg.Done()
			d.globalSem <- struct{}{}
			defer func() { <-d.globalSem }()

			resID, filePath, _, err := d.ProcessResource(sub.absoluteURL, d.guessResourceType(sub.absoluteURL), pageURL, headers)
			resultsCh <- cssSubResult{sub: sub, resID: resID, filePath: filePath, err: err}
		}(sub)
	}

	go func() {
		wg.Wait()
		close(resultsCh)
	}()

	for result := range resultsCh {
		if result.err != nil {
			log.Printf("Failed to process CSS resource %s: %v", result.sub.absoluteURL, result.err)
			continue
		}
		appendUniqueResourceID(resourceIDs, seen, result.resID)
		rewriter.AddMapping(result.sub.absoluteURL, result.filePath)
	}
}

func (d *Deduplicator) archiveFrameCapture(frame models.FrameCapture, headers map[string]string, pageID int64, timestamp string, frameMap map[string]models.FrameCapture, resourceIDs *[]int64, seen map[int64]struct{}, visiting map[string]bool, archived map[string]processedInlineHTML) (int64, string, error) {
	if cached, ok := archived[frame.URL]; ok {
		appendUniqueResourceID(resourceIDs, seen, cached.resourceID)
		return cached.resourceID, cached.filePath, nil
	}
	if visiting[frame.URL] {
		return 0, "", fmt.Errorf("cyclic iframe reference: %s", frame.URL)
	}
	visiting[frame.URL] = true
	defer delete(visiting, frame.URL)

	rewrittenHTML, err := d.rewriteCapturedHTML(frame.HTML, frame.URL, headers, pageID, timestamp, frameMap, resourceIDs, seen, visiting, archived)
	if err != nil {
		return 0, "", err
	}

	resourceID, filePath, _, err := d.processInlineResource(frame.URL, "html", []byte(rewrittenHTML))
	if err != nil {
		return 0, "", err
	}

	archived[frame.URL] = processedInlineHTML{resourceID: resourceID, filePath: filePath}
	appendUniqueResourceID(resourceIDs, seen, resourceID)
	return resourceID, filePath, nil
}

func (d *Deduplicator) rewriteCapturedHTML(htmlContent, baseURL string, headers map[string]string, pageID int64, timestamp string, frameMap map[string]models.FrameCapture, resourceIDs *[]int64, seen map[int64]struct{}, visiting map[string]bool, archived map[string]processedInlineHTML) (string, error) {
	htmlResources := d.htmlExtractor.ExtractResources(htmlContent, baseURL)
	frameURLs := buildFrameURLSet(frameMap)
	rewriter := NewURLRewriter()
	rewriter.SetPageID(pageID)
	rewriter.SetTimestamp(timestamp)
	rewriter.SetBaseURL(baseURL)

	var cssWorkItems []cssWorkItem
	for _, res := range htmlResources {
		if res.Type == "html" {
			if _, ok := frameURLs[res.URL]; ok {
				// 这些 iframe 会在最后按 frame key 统一重写成 /archive/...，不能再当成普通资源下载。
				continue
			}
		}

		resourceID, filePath, data, err := d.ProcessResource(res.URL, res.Type, baseURL, headers)
		if err != nil {
			log.Printf("Failed to process resource %s: %v", res.URL, err)
			continue
		}

		appendUniqueResourceID(resourceIDs, seen, resourceID)
		rewriter.AddMapping(res.URL, filePath)

		if res.Type == "css" {
			cssData := data
			if cssData == nil && filePath != "" {
				if fileData, readErr := d.storage.ReadResource(filePath); readErr == nil {
					cssData = fileData
				} else {
					log.Printf("Failed to read CSS file for sub-resource extraction: %s: %v", filePath, readErr)
				}
			}
			if cssData != nil {
				cssWorkItems = append(cssWorkItems, cssWorkItem{cssContent: string(cssData), cssURL: res.URL})
			}
		}
	}

	d.processCSSWorkItems(cssWorkItems, baseURL, headers, rewriter, resourceIDs, seen)

	normalizedHTML := ResolveRelativeURLs(NormalizeHTMLURLs(htmlContent), baseURL)
	normalizedHTML = d.rewriteIframeTagsByKey(normalizedHTML, pageID, timestamp, headers, frameMap, resourceIDs, seen, visiting, archived)
	return rewriter.RewriteHTML(normalizedHTML), nil
}

// ProcessCapture 处理完整的页面捕获，返回 (pageID, action, error)
func (d *Deduplicator) ProcessCapture(req *models.CaptureRequest) (int64, string, error) {
	capturedAt := time.Now()
	contentHash := hashCaptureContent(req.HTML, req.Frames)

	existingPage, err := d.db.GetPageByURLAndHash(req.URL, contentHash)
	if err != nil {
		return 0, "", fmt.Errorf("check existing page failed: %w", err)
	}
	if existingPage != nil {
		log.Printf("Page content unchanged, updating last visited: %s (ID: %d)", req.URL, existingPage.ID)
		if err := d.db.UpdatePageLastVisited(existingPage.ID, capturedAt); err != nil {
			return 0, "", fmt.Errorf("update last visited failed: %w", err)
		}
		return existingPage.ID, models.ArchiveActionUnchanged, nil
	}

	tempHTMLPath, err := d.storage.SaveHTML(req.URL, req.HTML, capturedAt)
	if err != nil {
		return 0, "", fmt.Errorf("save temp html failed: %w", err)
	}

	pageID := int64(0)
	finalized := false
	defer func() {
		if finalized {
			return
		}
		if tempHTMLPath != "" {
			if err := d.storage.DeleteHTML(tempHTMLPath); err != nil {
				log.Printf("Failed to delete temporary HTML %s: %v", tempHTMLPath, err)
			}
		}
		if pageID != 0 {
			if err := d.db.DeletePage(pageID); err != nil {
				log.Printf("Failed to rollback page %d after capture error: %v", pageID, err)
			}
		}
	}()

	bodyText := ExtractBodyText(req.HTML)
	pageID, err = d.db.CreatePage(req.URL, req.Title, tempHTMLPath, contentHash, capturedAt)
	if err != nil {
		if deleteErr := d.storage.DeleteHTML(tempHTMLPath); deleteErr != nil {
			log.Printf("Failed to delete temporary HTML %s after create page error: %v", tempHTMLPath, deleteErr)
		}
		tempHTMLPath = ""
		return 0, "", fmt.Errorf("create page failed: %w", err)
	}

	if bodyText != "" {
		if err := d.db.UpdatePageBodyText(pageID, bodyText); err != nil {
			log.Printf("Failed to save body text for page %d: %v", pageID, err)
		}
	}

	log.Printf("Page created (ID: %d, hash: %s): %s", pageID, contentHash[:16], req.URL)
	if err := d.finalizeCreateCapture(pageID, tempHTMLPath, capturedAt, req, nil); err != nil {
		return 0, "", err
	}

	finalized = true
	return pageID, models.ArchiveActionCreated, nil
}

// UpdateCapture 更新已存在页面的捕获内容
// 策略：更新 page 记录的 html_path 和 content_hash，旧 HTML 文件加入删除队列（7 天后自动删除）
func (d *Deduplicator) UpdateCapture(pageID int64, req *models.CaptureRequest) (string, error) {
	return d.updateCapture(pageID, req, nil)
}

func (d *Deduplicator) updateCapture(pageID int64, req *models.CaptureRequest, staleCheck func() bool) (string, error) {
	startTime := time.Now()
	log.Printf("[Update] Starting update for page %d", pageID)
	if staleCheck != nil && staleCheck() {
		return "", errStalePageTask
	}

	// 1. 获取现有页面信息（用于继承 first_visited）
	page, err := d.db.GetPageByID(fmt.Sprintf("%d", pageID))
	if err != nil || page == nil {
		return "", fmt.Errorf("page not found: %d", pageID)
	}

	newContentHash := hashCaptureContent(req.HTML, req.Frames)

	// 3. 如果内容未变化，仅更新时间
	if newContentHash == page.ContentHash {
		if err := d.db.UpdatePageLastVisited(pageID, time.Now()); err != nil {
			return "", err
		}
		log.Printf("[Update] Content unchanged, took %v", time.Since(startTime))
		return models.ArchiveActionUnchanged, nil
	}

	capturedAt := time.Now()
	oldHTMLPath := page.HTMLPath // 保存旧路径用于日志记录

	frameMap := buildFrameCaptureMap(req.Frames)
	log.Printf("[Update] Processing capture with %d top-level resources and %d frames", len(d.htmlExtractor.ExtractResources(req.HTML, req.URL)), len(frameMap))

	// 保存新 HTML
	tempHTMLPath, err := d.storage.SaveHTML(req.URL, req.HTML, capturedAt)
	if err != nil {
		return "", fmt.Errorf("save html failed: %w", err)
	}
	cleanupTempHTML := true
	defer func() {
		if cleanupTempHTML {
			if err := d.storage.DeleteHTML(tempHTMLPath); err != nil {
				log.Printf("[Update] Failed to delete temporary HTML %s: %v", tempHTMLPath, err)
			}
		}
	}()

	// 提取正文纯文本，并在最终替换快照时和资源关联一起提交。
	bodyText := ExtractBodyText(req.HTML)

	// 生成时间戳用于资源路径
	timestamp := capturedAt.Format("20060102150405")

	var resourceIDs []int64
	processStart := time.Now()
	resourceIDSet := make(map[int64]struct{})
	rewrittenHTML, err := d.rewriteCapturedHTML(req.HTML, req.URL, req.Headers, pageID, timestamp, frameMap, &resourceIDs, resourceIDSet, make(map[string]bool), make(map[string]processedInlineHTML))
	if err != nil {
		return "", fmt.Errorf("rewrite html failed: %w", err)
	}
	log.Printf("[Update] Processed %d linked resources in %v", len(resourceIDs), time.Since(processStart))
	if staleCheck != nil && staleCheck() {
		return "", errStalePageTask
	}

	// 更新保存的 HTML 文件
	if err := d.storage.UpdateHTML(tempHTMLPath, rewrittenHTML); err != nil {
		return "", fmt.Errorf("update html failed: %w", err)
	}
	rewrittenHTML = "" // 释放重写后的 HTML

	if d.testBeforeUpdateCommit != nil {
		if err := d.testBeforeUpdateCommit(pageID, tempHTMLPath, resourceIDs); err != nil {
			return "", err
		}
	}

	bodyTextPtr := &bodyText

	if err := d.db.ReplacePageSnapshot(pageID, tempHTMLPath, newContentHash, req.Title, bodyTextPtr, resourceIDs); err != nil {
		return "", fmt.Errorf("replace page snapshot failed: %w", err)
	}
	cleanupTempHTML = false

	// 将旧 HTML 文件加入删除队列（保留 7 天后自动删除）
	if oldHTMLPath != tempHTMLPath {
		if err := d.deletionQueue.Add(oldHTMLPath, pageID); err != nil {
			log.Printf("[Update] Failed to add old HTML to deletion queue: %v", err)
		}
	}

	log.Printf("[Update] Page updated (ID: %d, old_hash: %s, new_hash: %s, old_html: %s, new_html: %s, %d resources, %v)",
		pageID, page.ContentHash[:16], newContentHash[:16], oldHTMLPath, tempHTMLPath, len(resourceIDs), time.Since(startTime))
	return models.ArchiveActionUpdated, nil
}

func (d *Deduplicator) finalizeCreateCapture(pageID int64, tempHTMLPath string, capturedAt time.Time, req *models.CaptureRequest, staleCheck func() bool) error {
	if staleCheck != nil && staleCheck() {
		return errStalePageTask
	}

	frameMap := buildFrameCaptureMap(req.Frames)
	log.Printf("Total resources to process: %d (frames: %d)", len(d.htmlExtractor.ExtractResources(req.HTML, req.URL)), len(frameMap))

	timestamp := capturedAt.Format("20060102150405")
	var resourceIDs []int64
	startTime := time.Now()
	resourceIDSet := make(map[int64]struct{})
	rewrittenHTML, err := d.rewriteCapturedHTML(req.HTML, req.URL, req.Headers, pageID, timestamp, frameMap, &resourceIDs, resourceIDSet, make(map[string]bool), make(map[string]processedInlineHTML))
	if err != nil {
		return fmt.Errorf("rewrite html failed: %w", err)
	}
	log.Printf("Resource processing completed: %d linked resources, took %v", len(resourceIDs), time.Since(startTime))

	if staleCheck != nil && staleCheck() {
		return errStalePageTask
	}

	if err := d.storage.UpdateHTML(tempHTMLPath, rewrittenHTML); err != nil {
		return fmt.Errorf("update html failed: %w", err)
	}

	if d.testBeforeCreateFinalize != nil {
		if err := d.testBeforeCreateFinalize(pageID, tempHTMLPath, resourceIDs); err != nil {
			return err
		}
	}

	if staleCheck != nil && staleCheck() {
		return errStalePageTask
	}

	if err := d.db.DeletePageResources(pageID); err != nil {
		return fmt.Errorf("clear page resources failed: %w", err)
	}
	if err := d.db.LinkPageResources(pageID, resourceIDs); err != nil {
		return fmt.Errorf("link page resources failed: %w", err)
	}

	return nil
}

// resolveURL resolves a relative URL against a base URL
func (d *Deduplicator) resolveURL(baseURL, relativeURL string) string {
	// If already absolute, return as-is
	if strings.HasPrefix(relativeURL, "http://") || strings.HasPrefix(relativeURL, "https://") {
		return relativeURL
	}

	base, err := url.Parse(baseURL)
	if err != nil {
		log.Printf("Failed to parse base URL %s: %v", baseURL, err)
		return relativeURL
	}

	rel, err := url.Parse(relativeURL)
	if err != nil {
		log.Printf("Failed to parse relative URL %s: %v", relativeURL, err)
		return relativeURL
	}

	resolved := base.ResolveReference(rel)
	return resolved.String()
}

// guessResourceType guesses the resource type from URL
func (d *Deduplicator) guessResourceType(url string) string {
	lower := strings.ToLower(url)

	if strings.HasSuffix(lower, ".css") {
		return "css"
	}
	if strings.HasSuffix(lower, ".js") {
		return "js"
	}
	if strings.HasSuffix(lower, ".woff") || strings.HasSuffix(lower, ".woff2") ||
		strings.HasSuffix(lower, ".ttf") || strings.HasSuffix(lower, ".otf") ||
		strings.HasSuffix(lower, ".eot") {
		return "font"
	}
	if strings.HasSuffix(lower, ".jpg") || strings.HasSuffix(lower, ".jpeg") ||
		strings.HasSuffix(lower, ".png") || strings.HasSuffix(lower, ".gif") ||
		strings.HasSuffix(lower, ".svg") || strings.HasSuffix(lower, ".webp") ||
		strings.HasSuffix(lower, ".ico") {
		return "image"
	}
	if strings.Contains(lower, ".html") || strings.Contains(lower, ".htm") || strings.Contains(lower, "/html/") {
		return "html"
	}

	return "other"
}

// CleanupOldHTML processes the deletion queue and removes HTML files older than retentionDays
func (d *Deduplicator) CleanupOldHTML(retentionDays int) error {
	if retentionDays <= 0 {
		return fmt.Errorf("retention days must be positive")
	}

	deletedCount, err := d.deletionQueue.ProcessDeletions(d.storage.baseDir, retentionDays)
	if err != nil {
		return fmt.Errorf("failed to process deletion queue: %w", err)
	}

	if deletedCount > 0 {
		log.Printf("[cleanup] removed %d superseded HTML files from deletion queue", deletedCount)
	} else {
		log.Printf("[cleanup] no superseded HTML files to remove")
	}

	// Clean up empty directories
	htmlDir := filepath.Join(d.storage.baseDir, "html")
	d.cleanupEmptyDirs(htmlDir)

	return nil
}

// cleanupEmptyDirs removes empty directories recursively
func (d *Deduplicator) cleanupEmptyDirs(root string) {
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || !info.IsDir() || path == root {
			return nil
		}

		entries, err := os.ReadDir(path)
		if err != nil {
			return nil
		}

		if len(entries) == 0 {
			os.Remove(path)
		}

		return nil
	})
}

// AddHTMLToDeletionQueue 将 HTML 文件加入删除队列（供外部调用）
func (d *Deduplicator) AddHTMLToDeletionQueue(htmlPath string, pageID int64) error {
	return d.deletionQueue.Add(htmlPath, pageID)
}
