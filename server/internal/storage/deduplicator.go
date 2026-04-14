package storage

import (
	"crypto/sha256"
	"encoding/hex"
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
)

type resourceCacheEntry struct {
	resourceID int64
	filePath   string
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
	}
	d.startCacheCleanupLoop()
	return d
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

// cacheMaxBytes 返回缓存大小上限（字节）
func (d *Deduplicator) cacheMaxBytes() int64 {
	return int64(d.config.MetadataCacheMB) * 1024 * 1024
}

func cacheEntrySize(key, filePath string) int64 {
	return int64(len(key) + len(filePath) + resourceCacheEntryOverhead)
}

// cacheStore 缓存资源元数据，超出大小限制时淘汰最旧的条目
func (d *Deduplicator) cacheStore(key string, resourceID int64, filePath string, data []byte) {
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
	// 检查缓存
	if entry, ok := d.cache.Load(url); ok {
		cached := entry.(*resourceCacheEntry)
		if time.Since(cached.cachedAt) < resourceCacheTTL {
			if err := d.db.UpdateResourceLastSeen(cached.resourceID); err != nil {
				log.Printf("Failed to update last_seen for cached resource: %v", err)
			}
			return cached.resourceID, cached.filePath, nil, nil
		}
		if old, loaded := d.cache.LoadAndDelete(url); loaded {
			d.cacheBytes.Add(-old.(*resourceCacheEntry).size)
		}
	}

	var data []byte    // 小文件有值，大文件 nil
	var tmpPath string // 大文件临时文件路径，小文件空
	var hash string
	var fileSize int64

	streamThreshold := int64(d.config.StreamThresholdKB) * 1024
	var err error
	data, hash, tmpPath, err = d.storage.DownloadResource(url, pageURL, headers, streamThreshold)
	if err != nil {
		log.Printf("Download failed for %s: %v, trying fallback", url, err)
		return d.processResourceFallback(url, err)
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

	// 检查是否已有相同 URL 的资源记录
	existingByURL, err := d.db.GetResourceByURL(url)
	if err != nil {
		return 0, "", nil, fmt.Errorf("db query by url failed: %w", err)
	}
	if existingByURL != nil {
		if err := d.db.UpdateResourceLastSeen(existingByURL.ID); err != nil {
			return 0, "", nil, err
		}
		d.cacheStore(url, existingByURL.ID, existingByURL.FilePath, data)
		return existingByURL.ID, existingByURL.FilePath, data, nil
	}

	// 检查是否有相同哈希的资源
	existingByHash, err := d.db.GetResourceByHash(hash)
	if err != nil {
		return 0, "", nil, fmt.Errorf("db query by hash failed: %w", err)
	}

	var filePath string
	if existingByHash != nil {
		filePath = existingByHash.FilePath
		log.Printf("Same content (hash: %s) different URL, reusing file: %s", hash[:16], url)
	} else if tmpPath != "" {
		// 大文件：从临时文件移动到资源目录（零拷贝）
		filePath, err = d.storage.SaveResourceFromFile(tmpPath, hash, resourceType)
		if err != nil {
			return 0, "", nil, fmt.Errorf("save from file failed: %w", err)
		}
		tmpPath = "" // 已被移走，阻止 defer 删除
	} else {
		// 小文件：从内存写入
		filePath, err = d.storage.SaveResource(data, hash, resourceType)
		if err != nil {
			return 0, "", nil, fmt.Errorf("save failed: %w", err)
		}
	}

	resourceID, err := d.db.CreateResourceIfNotExists(url, hash, resourceType, filePath, fileSize)
	if err != nil {
		return 0, "", nil, fmt.Errorf("db insert failed: %w", err)
	}

	log.Printf("New resource record (hash: %s): %s", hash[:16], url)
	d.cacheStore(url, resourceID, filePath, data)
	return resourceID, filePath, data, nil
}

// processResourceFallback 下载失败时的兜底逻辑
// 不读取文件内容到内存，仅返回 DB 中的路径信息（调用方按需从磁盘读取）
func (d *Deduplicator) processResourceFallback(url string, downloadErr error) (int64, string, []byte, error) {
	existing, dbErr := d.db.GetResourceByURL(url)
	if (dbErr != nil || existing == nil) && strings.Contains(url, "?") {
		urlPath := url[:strings.IndexByte(url, '?')]
		existing, dbErr = d.db.GetResourceByURLLike(urlPath + "%")
		if existing != nil {
			log.Printf("Fallback: found resource by URL path match: %s -> %s", url, existing.URL)
		}
	}
	if dbErr != nil || existing == nil {
		return 0, "", nil, fmt.Errorf("download failed and no fallback: %w", downloadErr)
	}
	log.Printf("Fallback: reusing previous resource (ID: %d) for: %s", existing.ID, url)
	if updateErr := d.db.UpdateResourceLastSeen(existing.ID); updateErr != nil {
		log.Printf("Failed to update last_seen for fallback resource: %v", updateErr)
	}
	// 不读取文件到内存、不缓存，避免大文件导致内存膨胀
	return existing.ID, existing.FilePath, nil, nil
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
		frameMap[frame.Key] = frame
	}
	return frameMap
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
	rewriter := NewURLRewriter()
	rewriter.SetPageID(pageID)
	rewriter.SetTimestamp(timestamp)
	rewriter.SetBaseURL(baseURL)

	var cssWorkItems []cssWorkItem
	for _, res := range htmlResources {
		if frame, ok := frameMap[res.URL]; ok {
			resourceID, filePath, err := d.archiveFrameCapture(frame, headers, pageID, timestamp, frameMap, resourceIDs, seen, visiting, archived)
			if err != nil {
				log.Printf("Failed to process iframe capture %s: %v", res.URL, err)
				continue
			}
			appendUniqueResourceID(resourceIDs, seen, resourceID)
			rewriter.AddMapping(res.URL, filePath)
			continue
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
	return rewriter.RewriteHTML(normalizedHTML), nil
}

// ProcessCapture 处理完整的页面捕获，返回 (pageID, action, error)
func (d *Deduplicator) ProcessCapture(req *models.CaptureRequest) (int64, string, error) {
	capturedAt := time.Now()

	contentHash := hashCaptureContent(req.HTML, req.Frames)

	// 检查是否存在相同 URL 和内容哈希的页面
	existingPage, err := d.db.GetPageByURLAndHash(req.URL, contentHash)
	if err != nil {
		return 0, "", fmt.Errorf("check existing page failed: %w", err)
	}

	if existingPage != nil {
		// 内容未变化，只更新最后访问时间
		log.Printf("Page content unchanged, updating last visited: %s (ID: %d)", req.URL, existingPage.ID)
		if err := d.db.UpdatePageLastVisited(existingPage.ID, capturedAt); err != nil {
			return 0, "", fmt.Errorf("update last visited failed: %w", err)
		}
		return existingPage.ID, models.ArchiveActionUnchanged, nil
	}

	frameMap := buildFrameCaptureMap(req.Frames)
	log.Printf("Total resources to process: %d (frames: %d)", len(d.htmlExtractor.ExtractResources(req.HTML, req.URL)), len(frameMap))

	// 先创建页面记录以获取 pageID（用于生成正确的资源路径）
	// 使用临时 HTML 创建页面
	tempHTMLPath, err := d.storage.SaveHTML(req.URL, req.HTML, capturedAt)
	if err != nil {
		return 0, "", fmt.Errorf("save temp html failed: %w", err)
	}

	// 提取正文纯文本（在释放 HTML 之前）
	bodyText := ExtractBodyText(req.HTML)

	pageID, err := d.db.CreatePage(req.URL, req.Title, tempHTMLPath, contentHash, capturedAt)
	if err != nil {
		return 0, "", fmt.Errorf("create page failed: %w", err)
	}

	// 保存正文纯文本（用于全文搜索）
	if bodyText != "" {
		if err := d.db.UpdatePageBodyText(pageID, bodyText); err != nil {
			log.Printf("Failed to save body text for page %d: %v", pageID, err)
		}
	}
	bodyText = "" // 释放正文文本

	log.Printf("Page created (ID: %d, hash: %s): %s", pageID, contentHash[:16], req.URL)

	// 生成时间戳用于资源路径
	timestamp := capturedAt.Format("20060102150405")

	var resourceIDs []int64
	startTime := time.Now()
	resourceIDSet := make(map[int64]struct{})
	rewrittenHTML, err := d.rewriteCapturedHTML(req.HTML, req.URL, req.Headers, pageID, timestamp, frameMap, &resourceIDs, resourceIDSet, make(map[string]bool), make(map[string]processedInlineHTML))
	if err != nil {
		return 0, "", fmt.Errorf("rewrite html failed: %w", err)
	}
	log.Printf("Resource processing completed: %d linked resources, took %v", len(resourceIDs), time.Since(startTime))

	// 更新保存的 HTML 文件（用重写后的内容替换临时内容）
	if err := d.storage.UpdateHTML(tempHTMLPath, rewrittenHTML); err != nil {
		return 0, "", fmt.Errorf("update html failed: %w", err)
	}
	rewrittenHTML = "" // 释放重写后的 HTML

	// 关联页面和资源
	for _, resourceID := range resourceIDs {
		if err := d.db.LinkPageResource(pageID, resourceID); err != nil {
			log.Printf("Failed to link resource: %v", err)
		}
	}

	return pageID, models.ArchiveActionCreated, nil
}

// UpdateCapture 更新已存在页面的捕获内容
// 策略：更新 page 记录的 html_path 和 content_hash，旧 HTML 文件加入删除队列（7 天后自动删除）
func (d *Deduplicator) UpdateCapture(pageID int64, req *models.CaptureRequest) (string, error) {
	startTime := time.Now()
	log.Printf("[Update] Starting update for page %d", pageID)

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

	// 4. 删除旧的页面资源关联
	if err := d.db.DeletePageResources(pageID); err != nil {
		return "", fmt.Errorf("failed to delete old page resources: %w", err)
	}

	frameMap := buildFrameCaptureMap(req.Frames)
	log.Printf("[Update] Processing capture with %d top-level resources and %d frames", len(d.htmlExtractor.ExtractResources(req.HTML, req.URL)), len(frameMap))

	// 保存新 HTML
	tempHTMLPath, err := d.storage.SaveHTML(req.URL, req.HTML, capturedAt)
	if err != nil {
		return "", fmt.Errorf("save html failed: %w", err)
	}

	// 提取正文纯文本并保存（在释放 req.HTML 之前）
	bodyText := ExtractBodyText(req.HTML)
	if bodyText != "" {
		if err := d.db.UpdatePageBodyText(pageID, bodyText); err != nil {
			log.Printf("Failed to save body text for page %d: %v", pageID, err)
		}
	}
	bodyText = "" // 释放正文文本

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

	// 更新保存的 HTML 文件
	if err := d.storage.UpdateHTML(tempHTMLPath, rewrittenHTML); err != nil {
		return "", fmt.Errorf("update html failed: %w", err)
	}
	rewrittenHTML = "" // 释放重写后的 HTML

	// 更新数据库记录
	if err := d.db.UpdatePageContent(pageID, tempHTMLPath, newContentHash, req.Title); err != nil {
		return "", fmt.Errorf("update page content failed: %w", err)
	}

	// 关联新资源
	for _, resourceID := range resourceIDs {
		if err := d.db.LinkPageResource(pageID, resourceID); err != nil {
			log.Printf("[Update] Failed to link resource: %v", err)
		}
	}

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
