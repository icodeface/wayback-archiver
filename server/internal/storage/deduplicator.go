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

type keyedMutexEntry struct {
	mu   sync.Mutex
	refs int
}

type keyedMutex struct {
	mu    sync.Mutex
	locks map[string]*keyedMutexEntry
}

func newKeyedMutex() *keyedMutex {
	return &keyedMutex{locks: make(map[string]*keyedMutexEntry)}
}

func (m *keyedMutex) lock(key string) func() {
	m.mu.Lock()
	entry := m.locks[key]
	if entry == nil {
		entry = &keyedMutexEntry{}
		m.locks[key] = entry
	}
	entry.refs++
	m.mu.Unlock()

	entry.mu.Lock()

	return func() {
		entry.mu.Unlock()

		m.mu.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(m.locks, key)
		}
		m.mu.Unlock()
	}
}

type Deduplicator struct {
	db            database.Database
	storage       *FileStorage
	cssParser     *CSSParser
	htmlExtractor *HTMLResourceExtractor
	cache         sync.Map // url -> *resourceCacheEntry
	deletionQueue *DeletionQueue
	config        config.ResourceConfig
	cacheBytes    atomic.Int64  // 当前缓存占用字节数
	cacheMu       sync.Mutex    // 保护缓存淘汰逻辑，防止并发 cacheStore 导致超限
	globalSem     chan struct{} // 全局并发下载信号量，跨所有页面共享
	pageCreateMu  *keyedMutex
	resourceMu    *keyedMutex

	// 拆分后的职责组件。旧字段和方法保留作为兼容 façade，逐步迁移调用方。
	concurrencyManager   *ConcurrencyManager
	resourceCache        *ResourceCache
	resourceCacheOnce    sync.Once
	resourceDownloader   *ResourceDownloader
	resourceDeduplicator *ResourceDeduplicator
	htmlRewriter         *HTMLRewriter
	pageArchiver         *PageArchiver

	// 测试钩子：用于稳定复现页面已创建/新 HTML 已写入后的失败路径。
	testBeforeCreateFinalize func(pageID int64, htmlPath string, resourceIDs []int64) error
	testBeforeUpdateCommit   func(pageID int64, htmlPath string, resourceIDs []int64) error
	testBeforePageCreate     func(url, contentHash string)
	testBeforeResourceCreate func(url string)
}

func NewDeduplicator(db database.Database, storage *FileStorage, cfg config.ResourceConfig) *Deduplicator {
	concurrency := NewConcurrencyManager(cfg.Workers)
	d := &Deduplicator{
		db:                 db,
		storage:            storage,
		cssParser:          NewCSSParser(),
		htmlExtractor:      NewHTMLResourceExtractor(),
		deletionQueue:      NewDeletionQueue(storage.baseDir),
		config:             cfg,
		globalSem:          concurrency.downloadSem,
		pageCreateMu:       concurrency.pageMu,
		resourceMu:         concurrency.resourceMu,
		concurrencyManager: concurrency,
	}
	d.resourceCache = newResourceCacheWithBacking(cfg.MetadataCacheMB, &d.cache, &d.cacheMu, &d.cacheBytes)
	d.resourceDownloader = NewResourceDownloader(storage, concurrency)
	d.resourceDeduplicator = NewResourceDeduplicator(db, storage, d.resourceCache, d.resourceDownloader, concurrency)
	d.resourceDeduplicator.streamThresholdKB = cfg.StreamThresholdKB
	d.resourceDeduplicator.beforeCreate = func(url string) {
		if d.testBeforeResourceCreate != nil {
			d.testBeforeResourceCreate(url)
		}
	}
	d.htmlRewriter = NewHTMLRewriter(func(req HTMLRewriteRequest) (HTMLRewriteResult, error) {
		frameMap := buildFrameCaptureMap(req.Frames)
		resourceIDs := make([]int64, 0)
		seen := make(map[int64]struct{})
		html, err := d.rewriteCapturedHTML(req.HTML, req.BaseURL, req.Headers, req.Cookies, req.PageID, req.Timestamp, frameMap, &resourceIDs, seen, make(map[string]bool), make(map[string]processedInlineHTML))
		return HTMLRewriteResult{HTML: html, ResourceIDs: resourceIDs}, err
	})
	d.pageArchiver = NewPageArchiver(d)
	d.startCacheCleanupLoop()
	return d
}

// PageArchiver 返回页面归档 orchestrator。返回同一个实例，便于服务层复用。
func (d *Deduplicator) PageArchiver() *PageArchiver {
	if d.pageArchiver == nil {
		d.pageArchiver = NewPageArchiver(d)
	}
	return d.pageArchiver
}

func (d *Deduplicator) ResourceDownloader() *ResourceDownloader     { return d.resourceDownloader }
func (d *Deduplicator) ResourceDeduplicator() *ResourceDeduplicator { return d.resourceDeduplicator }
func (d *Deduplicator) ResourceCache() *ResourceCache               { return d.ensureResourceCache() }
func (d *Deduplicator) HTMLRewriter() *HTMLRewriter                 { return d.htmlRewriter }
func (d *Deduplicator) ConcurrencyManager() *ConcurrencyManager     { return d.concurrencyManager }

var errStalePageTask = errors.New("stale page task")

var ErrCaptureURLMismatch = errors.New("capture request URL does not match page URL")

// Public compatibility façade. New code should prefer PageArchiver,
// ResourceDeduplicator and the other focused components.
func (d *Deduplicator) ProcessCapture(req *models.CaptureRequest) (int64, string, error) {
	return d.processCapture(req)
}
func (d *Deduplicator) ProcessCaptureAsync(req *models.CaptureRequest) (int64, string, error) {
	return d.processCaptureAsync(req)
}
func (d *Deduplicator) UpdateCapture(pageID int64, req *models.CaptureRequest) (string, error) {
	return d.updateCaptureSync(pageID, req)
}
func (d *Deduplicator) UpdateCaptureAsync(pageID int64, req *models.CaptureRequest) (string, error) {
	return d.updateCaptureAsync(pageID, req)
}
func (d *Deduplicator) ProcessResource(url, resourceType, pageURL string, headers map[string]string, cookies []models.CaptureCookie) (int64, string, []byte, error) {
	return d.processResource(url, resourceType, pageURL, headers, cookies)
}

func (d *Deduplicator) getPageForUpdate(pageID int64, reqURL string) (*models.Page, error) {
	page, err := d.db.GetPageByID(fmt.Sprintf("%d", pageID))
	if err != nil || page == nil {
		return nil, fmt.Errorf("page not found: %d", pageID)
	}
	if page.URL != reqURL {
		return nil, fmt.Errorf("%w: page %d has %q, request has %q", ErrCaptureURLMismatch, pageID, page.URL, reqURL)
	}
	return page, nil
}

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
	if len(req.Cookies) > 0 {
		cloned.Cookies = append([]models.CaptureCookie(nil), req.Cookies...)
	}
	return cloned
}

func (d *Deduplicator) nextPageTaskSeq(pageID int64) uint64 {
	return d.concurrencyManager.NextPageTask(pageID)
}

func (d *Deduplicator) isLatestPageTask(pageID int64, seq uint64) bool {
	return d.concurrencyManager.IsLatestPageTask(pageID, seq)
}

func (d *Deduplicator) runPageTask(pageID int64, seq uint64, label string, fn func() error, onError func(error)) {
	d.concurrencyManager.RunPageTask(pageID, seq, label, fn, onError)
}

func (d *Deduplicator) WaitForBackgroundTasks() {
	d.concurrencyManager.WaitForBackgroundTasks()
}

func pageCreateKey(url, contentHash string) string {
	return url + "\x00" + contentHash
}

type pageCreatePreparation struct {
	pageID            int64
	action            string
	tempHTMLPath      string
	enqueueFinalize   bool
	rollbackOnFailure bool
}

func (d *Deduplicator) preparePageCreate(req *models.CaptureRequest, capturedAt time.Time, contentHash string) (*pageCreatePreparation, error) {
	unlock := d.concurrencyManager.LockPage(pageCreateKey(req.URL, contentHash))
	defer unlock()

	existingPage, err := d.db.GetPageByURLAndHash(req.URL, contentHash)
	if err != nil {
		return nil, fmt.Errorf("check existing page failed: %w", err)
	}
	if existingPage != nil {
		if existingPage.SnapshotState == models.SnapshotStateReady {
			log.Printf("Page content unchanged, updating last visited: %s (ID: %d)", req.URL, existingPage.ID)
			if err := d.db.UpdatePageLastVisited(existingPage.ID, capturedAt); err != nil {
				return nil, fmt.Errorf("update last visited failed: %w", err)
			}
			return &pageCreatePreparation{pageID: existingPage.ID, action: models.ArchiveActionUnchanged}, nil
		}
		if existingPage.SnapshotState == models.SnapshotStatePending {
			log.Printf("Page finalize already in progress, reusing pending page: %s (ID: %d)", req.URL, existingPage.ID)
			if err := d.db.UpdatePageLastVisited(existingPage.ID, capturedAt); err != nil {
				return nil, fmt.Errorf("update pending page last visited failed: %w", err)
			}
			return &pageCreatePreparation{pageID: existingPage.ID, action: models.ArchiveActionCreated}, nil
		}
	}

	tempHTMLPath, err := d.storage.SaveHTML(req.URL, req.HTML, capturedAt)
	if err != nil {
		return nil, fmt.Errorf("save temp html failed: %w", err)
	}
	bodyText := ExtractBodyText(req.HTML)

	if existingPage != nil {
		oldHTMLPath, err := d.db.ResetPageForCreateRetry(existingPage.ID, req.Title, tempHTMLPath, capturedAt)
		if err != nil {
			if deleteErr := d.storage.DeleteHTML(tempHTMLPath); deleteErr != nil {
				log.Printf("Failed to delete temporary HTML %s after retry reset error: %v", tempHTMLPath, deleteErr)
			}
			return nil, fmt.Errorf("reset incomplete page failed: %w", err)
		}
		if bodyText != "" {
			if err := d.db.UpdatePageBodyText(existingPage.ID, bodyText); err != nil {
				log.Printf("Failed to save body text for retried page %d: %v", existingPage.ID, err)
			}
		}
		if oldHTMLPath != "" && oldHTMLPath != tempHTMLPath {
			if err := d.storage.DeleteHTML(oldHTMLPath); err != nil {
				log.Printf("Failed to delete superseded HTML %s for page %d: %v", oldHTMLPath, existingPage.ID, err)
			}
		}
		log.Printf("Retrying incomplete page finalize: %s (ID: %d, state: %s)", req.URL, existingPage.ID, existingPage.SnapshotState)
		return &pageCreatePreparation{pageID: existingPage.ID, action: models.ArchiveActionCreated, tempHTMLPath: tempHTMLPath, enqueueFinalize: true}, nil
	}

	if d.testBeforePageCreate != nil {
		d.testBeforePageCreate(req.URL, contentHash)
	}

	pageID, err := d.db.CreatePage(req.URL, req.Title, tempHTMLPath, contentHash, capturedAt)
	if err != nil {
		if deleteErr := d.storage.DeleteHTML(tempHTMLPath); deleteErr != nil {
			log.Printf("Failed to delete temporary HTML %s after create page error: %v", tempHTMLPath, deleteErr)
		}
		return nil, fmt.Errorf("create page failed: %w", err)
	}

	if bodyText != "" {
		if err := d.db.UpdatePageBodyText(pageID, bodyText); err != nil {
			log.Printf("Failed to save body text for page %d: %v", pageID, err)
		}
	}

	return &pageCreatePreparation{pageID: pageID, action: models.ArchiveActionCreated, tempHTMLPath: tempHTMLPath, enqueueFinalize: true, rollbackOnFailure: true}, nil
}

func (d *Deduplicator) processCaptureAsync(req *models.CaptureRequest) (int64, string, error) {
	capturedAt := time.Now()
	contentHash := hashCaptureContent(req.HTML, req.Frames)

	prep, err := d.preparePageCreate(req, capturedAt, contentHash)
	if err != nil {
		return 0, "", err
	}
	if prep.action == models.ArchiveActionUnchanged {
		return prep.pageID, prep.action, nil
	}
	if !prep.enqueueFinalize {
		return prep.pageID, prep.action, nil
	}

	seq := d.nextPageTaskSeq(prep.pageID)
	clonedReq := cloneCaptureRequest(req)
	d.runPageTask(prep.pageID, seq, "Create", func() error {
		staleCheck := func() bool { return !d.isLatestPageTask(prep.pageID, seq) }
		err := d.finalizeCreateCapture(prep.pageID, prep.tempHTMLPath, capturedAt, clonedReq, staleCheck)
		if errors.Is(err, errStalePageTask) {
			if deleteErr := d.storage.DeleteHTML(prep.tempHTMLPath); deleteErr != nil {
				log.Printf("[Create] Failed to delete stale HTML %s for page %d: %v", prep.tempHTMLPath, prep.pageID, deleteErr)
			}
		}
		return err
	}, func(err error) {
		if !d.isLatestPageTask(prep.pageID, seq) {
			return
		}
		if markErr := d.db.MarkPageCreateFailed(prep.pageID); markErr != nil {
			log.Printf("[Create] Failed to mark page %d as failed after %v: %v", prep.pageID, err, markErr)
		}
	})

	log.Printf("Page created (ID: %d, hash: %s): %s", prep.pageID, contentHash[:16], req.URL)
	return prep.pageID, models.ArchiveActionCreated, nil
}

func (d *Deduplicator) updateCaptureAsync(pageID int64, req *models.CaptureRequest) (string, error) {
	page, err := d.getPageForUpdate(pageID, req.URL)
	if err != nil {
		return "", err
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
	}, nil)

	return models.ArchiveActionUpdated, nil
}

func (d *Deduplicator) startCacheCleanupLoop() {
	d.ensureResourceCache().StartCleanup()
}

func (d *Deduplicator) cleanupExpiredCache() int {
	removed := d.ensureResourceCache().CleanupExpired()

	if removed > 0 {
		log.Printf("[cache] evicted %d expired resource entries", removed)
	}

	return removed
}

func (d *Deduplicator) cacheDelete(key string) {
	d.ensureResourceCache().Delete(key)
}

func (d *Deduplicator) loadCachedResource(url string) *resourceCacheEntry {
	return d.ensureResourceCache().Load(url)
}

func (d *Deduplicator) ensureResourceCache() *ResourceCache {
	d.resourceCacheOnce.Do(func() {
		if d.resourceCache == nil {
			d.resourceCache = newResourceCacheWithBacking(d.config.MetadataCacheMB, &d.cache, &d.cacheMu, &d.cacheBytes)
		}
	})
	return d.resourceCache
}

func shortHashForLog(hash string) string {
	if len(hash) <= 16 {
		return hash
	}
	return hash[:16]
}

func (d *Deduplicator) quarantineResourceFile(filePath, reason string) error {
	quarantinePath := filePath
	fullPath := filepath.Join(d.storage.baseDir, filePath)
	if _, err := os.Stat(fullPath); err == nil {
		copiedPath, copyErr := d.storage.CreateResourceQuarantineCopy(filePath)
		if copyErr != nil {
			return fmt.Errorf("create quarantine copy failed: %w", copyErr)
		}
		quarantinePath = copiedPath
	}

	affected, err := d.db.QuarantineResourcesByFilePath(filePath, quarantinePath, reason)
	if err != nil {
		if quarantinePath != filePath {
			_ = os.Remove(filepath.Join(d.storage.baseDir, quarantinePath))
		}
		return fmt.Errorf("update quarantine metadata failed: %w", err)
	}
	if quarantinePath != filePath {
		if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove corrupted file failed: %w", err)
		}
	}
	log.Printf("[resource] quarantined corrupted file=%s moved_to=%s affected=%d reason=%s", filePath, quarantinePath, affected, reason)
	return nil
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
	return d.ensureResourceCache().MaxBytes()
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
	d.ensureResourceCache().Store(key, resourceID, filePath, metadata)
}

// ProcessResource 处理单个资源：下载、去重、存储
// 返回 (resourceID, filePath, data, error)
// 小文件（≤ streamThreshold）保留在内存供当前调用链使用；缓存只保留元数据
func (d *Deduplicator) processResource(url, resourceType string, pageURL string, headers map[string]string, cookies []models.CaptureCookie) (int64, string, []byte, error) {
	result, err := d.resourceDeduplicator.Process(ResourceProcessRequest{URL: url, Type: resourceType, PageURL: pageURL, Headers: headers, Cookies: cookies})
	return result.ResourceID, result.FilePath, result.Data, err
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

// processResourceFallback 下载失败时的兜底逻辑。
// 仅允许复用同一 URL 的历史资源，并要求本地文件仍然存在，避免跨 URL 误复用。
type cssWorkItem struct {
	cssContent string
	cssURL     string
}

type processedInlineHTML struct {
	resourceID int64
	filePath   string
}

func frameProcessingKey(frame models.FrameCapture) string {
	if frame.Key != "" {
		return frame.Key
	}
	return frame.URL
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

func (d *Deduplicator) rewriteIframeTagsByKey(htmlContent string, pageID int64, timestamp string, headers map[string]string, cookies []models.CaptureCookie, frameMap map[string]models.FrameCapture, resourceIDs *[]int64, seen map[int64]struct{}, visiting map[string]bool, archived map[string]processedInlineHTML) string {
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

		resourceID, _, err := d.archiveFrameCapture(frame, headers, cookies, pageID, timestamp, frameMap, resourceIDs, seen, visiting, archived)
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
	unlock := d.concurrencyManager.LockResource(url)
	defer unlock()
	if d.testBeforeResourceCreate != nil {
		d.testBeforeResourceCreate(url)
	}

	hashBytes := sha256.Sum256(data)
	hash := hex.EncodeToString(hashBytes[:])
	fileSize := int64(len(data))

	existingByHash, err := d.db.GetResourceByHash(hash)
	if err != nil {
		return 0, "", nil, fmt.Errorf("db query by hash failed: %w", err)
	}

	var filePath string
	if existingByHash != nil {
		if d.resourceDeduplicator.ensureReusableResource(existingByHash, url) {
			filePath = existingByHash.FilePath
		} else {
			existingByHash = nil
		}
	}
	if existingByHash == nil {
		filePath, err = d.storage.SaveResource(data, hash, resourceType)
		if err != nil {
			return 0, "", nil, fmt.Errorf("save failed: %w", err)
		}
	}

	resourceID, err := d.db.CreateResource(url, hash, resourceType, filePath, fileSize)
	if err != nil {
		return 0, "", nil, fmt.Errorf("db insert failed: %w", err)
	}

	d.cacheStore(url, resourceID, filePath, data)
	return resourceID, filePath, data, nil
}

func (d *Deduplicator) processCSSWorkItems(cssWorkItems []cssWorkItem, pageURL string, headers map[string]string, cookies []models.CaptureCookie, rewriter *URLRewriter, resourceIDs *[]int64, seen map[int64]struct{}) {
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
			processed, err := d.resourceDeduplicator.Process(ResourceProcessRequest{URL: sub.absoluteURL, Type: d.guessResourceType(sub.absoluteURL), PageURL: pageURL, Headers: headers, Cookies: cookies})
			resultsCh <- cssSubResult{sub: sub, resID: processed.ResourceID, filePath: processed.FilePath, err: err}
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

func (d *Deduplicator) archiveFrameCapture(frame models.FrameCapture, headers map[string]string, cookies []models.CaptureCookie, pageID int64, timestamp string, frameMap map[string]models.FrameCapture, resourceIDs *[]int64, seen map[int64]struct{}, visiting map[string]bool, archived map[string]processedInlineHTML) (int64, string, error) {
	cacheKey := frameProcessingKey(frame)
	if cached, ok := archived[cacheKey]; ok {
		appendUniqueResourceID(resourceIDs, seen, cached.resourceID)
		return cached.resourceID, cached.filePath, nil
	}
	if visiting[cacheKey] {
		return 0, "", fmt.Errorf("cyclic iframe reference: %s", frame.URL)
	}
	visiting[cacheKey] = true
	defer delete(visiting, cacheKey)

	rewrittenHTML, err := d.rewriteCapturedHTML(frame.HTML, frame.URL, headers, cookies, pageID, timestamp, frameMap, resourceIDs, seen, visiting, archived)
	if err != nil {
		return 0, "", err
	}

	resourceID, filePath, _, err := d.processInlineResource(frame.URL, "html", []byte(rewrittenHTML))
	if err != nil {
		return 0, "", err
	}

	archived[cacheKey] = processedInlineHTML{resourceID: resourceID, filePath: filePath}
	appendUniqueResourceID(resourceIDs, seen, resourceID)
	return resourceID, filePath, nil
}

func (d *Deduplicator) rewriteCapturedHTML(htmlContent, baseURL string, headers map[string]string, cookies []models.CaptureCookie, pageID int64, timestamp string, frameMap map[string]models.FrameCapture, resourceIDs *[]int64, seen map[int64]struct{}, visiting map[string]bool, archived map[string]processedInlineHTML) (string, error) {
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

		processed, err := d.resourceDeduplicator.Process(ResourceProcessRequest{URL: res.URL, Type: res.Type, PageURL: baseURL, Headers: headers, Cookies: cookies})
		if err != nil {
			log.Printf("Failed to process resource %s: %v", res.URL, err)
			continue
		}
		resourceID, filePath, data := processed.ResourceID, processed.FilePath, processed.Data

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

	d.processCSSWorkItems(cssWorkItems, baseURL, headers, cookies, rewriter, resourceIDs, seen)

	normalizedHTML := ResolveRelativeURLs(NormalizeHTMLURLs(htmlContent), baseURL)
	normalizedHTML = d.rewriteIframeTagsByKey(normalizedHTML, pageID, timestamp, headers, cookies, frameMap, resourceIDs, seen, visiting, archived)
	return rewriter.RewriteHTML(normalizedHTML), nil
}

// ProcessCapture 处理完整的页面捕获，返回 (pageID, action, error)
func (d *Deduplicator) processCapture(req *models.CaptureRequest) (int64, string, error) {
	capturedAt := time.Now()
	contentHash := hashCaptureContent(req.HTML, req.Frames)

	prep, err := d.preparePageCreate(req, capturedAt, contentHash)
	if err != nil {
		return 0, "", err
	}
	if prep.action == models.ArchiveActionUnchanged {
		return prep.pageID, prep.action, nil
	}
	if !prep.enqueueFinalize {
		return prep.pageID, prep.action, nil
	}

	finalized := false
	defer func() {
		if finalized {
			return
		}
		if prep.rollbackOnFailure {
			if prep.tempHTMLPath != "" {
				if err := d.storage.DeleteHTML(prep.tempHTMLPath); err != nil {
					log.Printf("Failed to delete temporary HTML %s: %v", prep.tempHTMLPath, err)
				}
			}
			if prep.pageID != 0 {
				if err := d.db.DeletePage(prep.pageID); err != nil {
					log.Printf("Failed to rollback page %d after capture error: %v", prep.pageID, err)
				}
			}
			return
		}
		if prep.pageID != 0 {
			if err := d.db.MarkPageCreateFailed(prep.pageID); err != nil {
				log.Printf("Failed to mark page %d as failed after capture error: %v", prep.pageID, err)
			}
		}
	}()

	log.Printf("Page created (ID: %d, hash: %s): %s", prep.pageID, contentHash[:16], req.URL)
	if err := d.finalizeCreateCapture(prep.pageID, prep.tempHTMLPath, capturedAt, req, nil); err != nil {
		return 0, "", err
	}

	finalized = true
	return prep.pageID, models.ArchiveActionCreated, nil
}

// UpdateCapture 更新已存在页面的捕获内容
// 策略：更新 page 记录的 html_path 和 content_hash，旧 HTML 文件加入删除队列（7 天后自动删除）
func (d *Deduplicator) updateCaptureSync(pageID int64, req *models.CaptureRequest) (string, error) {
	return d.updateCapture(pageID, req, nil)
}

func (d *Deduplicator) updateCapture(pageID int64, req *models.CaptureRequest, staleCheck func() bool) (string, error) {
	startTime := time.Now()
	log.Printf("[Update] Starting update for page %d", pageID)
	if staleCheck != nil && staleCheck() {
		return "", errStalePageTask
	}

	// 1. 获取现有页面信息（用于继承 first_visited）
	page, err := d.getPageForUpdate(pageID, req.URL)
	if err != nil {
		return "", err
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

	log.Printf("[Update] Processing capture with %d top-level resources and %d frames", len(d.htmlExtractor.ExtractResources(req.HTML, req.URL)), len(req.Frames))

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

	processStart := time.Now()
	rewriteResult, err := d.htmlRewriter.Rewrite(HTMLRewriteRequest{HTML: req.HTML, BaseURL: req.URL, Headers: req.Headers, Cookies: req.Cookies, Frames: req.Frames, PageID: pageID, Timestamp: timestamp})
	if err != nil {
		return "", fmt.Errorf("rewrite html failed: %w", err)
	}
	resourceIDs := rewriteResult.ResourceIDs
	rewrittenHTML := rewriteResult.HTML
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

	log.Printf("Total resources to process: %d (frames: %d)", len(d.htmlExtractor.ExtractResources(req.HTML, req.URL)), len(req.Frames))

	timestamp := capturedAt.Format("20060102150405")
	startTime := time.Now()
	rewriteResult, err := d.htmlRewriter.Rewrite(HTMLRewriteRequest{HTML: req.HTML, BaseURL: req.URL, Headers: req.Headers, Cookies: req.Cookies, Frames: req.Frames, PageID: pageID, Timestamp: timestamp})
	if err != nil {
		return fmt.Errorf("rewrite html failed: %w", err)
	}
	resourceIDs := rewriteResult.ResourceIDs
	rewrittenHTML := rewriteResult.HTML
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

	if err := d.db.FinalizePageCreate(pageID, resourceIDs); err != nil {
		return fmt.Errorf("finalize page create failed: %w", err)
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

	deletedCount, err := d.deletionQueue.ProcessDeletionsWithProtection(d.storage.baseDir, retentionDays, func(record DeletionRecord) (bool, error) {
		return d.db.HasActiveShareForHTMLPath(record.HTMLPath)
	})
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
