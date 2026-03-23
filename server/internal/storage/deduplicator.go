package storage

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"wayback/internal/config"
	"wayback/internal/database"
	"wayback/internal/models"
)

const (
	resourceCacheTTL = 2 * time.Hour
)

type resourceCacheEntry struct {
	resourceID int64
	filePath   string
	data       []byte
	size       int64 // len(data) 用于统计缓存大小
	cachedAt   time.Time
}

type Deduplicator struct {
	db            *database.DB
	storage       *FileStorage
	cssParser     *CSSParser
	htmlExtractor *HTMLResourceExtractor
	cache         sync.Map  // url -> *resourceCacheEntry
	deletionQueue *DeletionQueue
	config        config.ResourceConfig
	cacheBytes    atomic.Int64 // 当前缓存占用字节数
	globalSem     chan struct{} // 全局并发下载信号量，跨所有页面共享
}

func NewDeduplicator(db *database.DB, storage *FileStorage, cfg config.ResourceConfig) *Deduplicator {
	return &Deduplicator{
		db:            db,
		storage:       storage,
		cssParser:     NewCSSParser(),
		htmlExtractor: NewHTMLResourceExtractor(),
		deletionQueue: NewDeletionQueue(storage.baseDir),
		config:        cfg,
		globalSem:     make(chan struct{}, cfg.Workers),
	}
}

// cacheMaxBytes 返回缓存大小上限（字节）
func (d *Deduplicator) cacheMaxBytes() int64 {
	return int64(d.config.CacheSizeMB) * 1024 * 1024
}

// cacheStore 缓存资源，超出大小限制时淘汰最旧的条目
func (d *Deduplicator) cacheStore(key string, resourceID int64, filePath string, data []byte) {
	entrySize := int64(len(data))

	// 如果单个条目就超过缓存上限，不缓存
	if entrySize > d.cacheMaxBytes() {
		return
	}

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
				d.cache.Delete(k)
				d.cacheBytes.Add(-entry.size)
				evicted = true
				return false // 淘汰一个后重新检查
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
		data:       data,
		size:       entrySize,
		cachedAt:   time.Now(),
	}
	d.cache.Store(key, entry)
	d.cacheBytes.Add(entrySize)
}

// ProcessResource 处理单个资源：下载、去重、存储
// 返回 (resourceID, filePath, data, error)
// 小文件（≤ streamThreshold）保留在内存并缓存 data；大文件流式写入临时文件，data 返回 nil
func (d *Deduplicator) ProcessResource(url, resourceType, base64Content string, pageURL string, headers map[string]string) (int64, string, []byte, error) {
	// 检查缓存（仅当没有提供 base64 内容时）
	if base64Content == "" {
		if entry, ok := d.cache.Load(url); ok {
			cached := entry.(*resourceCacheEntry)
			if time.Since(cached.cachedAt) < resourceCacheTTL {
				if err := d.db.UpdateResourceLastSeen(cached.resourceID); err != nil {
					log.Printf("Failed to update last_seen for cached resource: %v", err)
				}
				return cached.resourceID, cached.filePath, cached.data, nil
			}
			d.cache.Delete(url)
			d.cacheBytes.Add(-cached.size)
		}
	}

	var data []byte    // 小文件有值，大文件 nil
	var tmpPath string // 大文件临时文件路径，小文件空
	var hash string
	var fileSize int64

	if base64Content != "" {
		var err error
		data, err = base64.StdEncoding.DecodeString(base64Content)
		if err != nil {
			return 0, "", nil, fmt.Errorf("base64 decode failed: %w", err)
		}
		hashBytes := sha256.Sum256(data)
		hash = hex.EncodeToString(hashBytes[:])
		fileSize = int64(len(data))
	} else {
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
	fileData, readErr := d.storage.ReadResource(existing.FilePath)
	if readErr != nil {
		return 0, "", nil, fmt.Errorf("download failed and fallback read failed: %w", downloadErr)
	}
	log.Printf("Fallback: reusing previous resource (ID: %d) for: %s", existing.ID, url)
	if updateErr := d.db.UpdateResourceLastSeen(existing.ID); updateErr != nil {
		log.Printf("Failed to update last_seen for fallback resource: %v", updateErr)
	}
	d.cacheStore(url, existing.ID, existing.FilePath, fileData)
	return existing.ID, existing.FilePath, fileData, nil
}

// ProcessCapture 处理完整的页面捕获，返回 (pageID, action, error)
func (d *Deduplicator) ProcessCapture(req *models.CaptureRequest) (int64, string, error) {
	capturedAt := time.Now()

	// 计算 HTML 内容哈希
	htmlHash := sha256.Sum256([]byte(req.HTML))
	contentHash := hex.EncodeToString(htmlHash[:])

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

	// 内容有变化或首次访问，创建新记录

	// 从 HTML 中提取所有资源 URL
	htmlResources := d.htmlExtractor.ExtractResources(req.HTML, req.URL)

	allResources := make([]models.ResourceReference, 0, len(htmlResources))
	for _, res := range htmlResources {
		allResources = append(allResources, models.ResourceReference{
			URL:  res.URL,
			Type: res.Type,
		})
	}

	log.Printf("Total resources to process: %d", len(allResources))

	// 先创建页面记录以获取 pageID（用于生成正确的资源路径）
	// 使用临时 HTML 创建页面
	tempHTMLPath, err := d.storage.SaveHTML(req.URL, req.HTML, capturedAt)
	if err != nil {
		return 0, "", fmt.Errorf("save temp html failed: %w", err)
	}

	pageID, err := d.db.CreatePage(req.URL, req.Title, tempHTMLPath, contentHash, capturedAt)
	if err != nil {
		return 0, "", fmt.Errorf("create page failed: %w", err)
	}

	// 提取正文纯文本并保存（用于全文搜索）
	bodyText := ExtractBodyText(req.HTML)
	if bodyText != "" {
		if err := d.db.UpdatePageBodyText(pageID, bodyText); err != nil {
			log.Printf("Failed to save body text for page %d: %v", pageID, err)
		}
	}

	log.Printf("Page created (ID: %d, hash: %s): %s", pageID, contentHash[:16], req.URL)

	// 生成时间戳用于资源路径
	timestamp := capturedAt.Format("20060102150405")

	// 创建 URL 重写器
	rewriter := NewURLRewriter()
	rewriter.SetPageID(pageID)
	rewriter.SetTimestamp(timestamp)
	rewriter.SetBaseURL(req.URL)

	var resourceIDs []int64
	cssURLMapping := make(map[string]string) // CSS URL -> local path mapping

	// 并行处理资源
	type resourceResult struct {
		res        models.ResourceReference
		resourceID int64
		data       []byte
		filePath   string
		err        error
	}

	resultsCh := make(chan resourceResult, len(allResources))
	sem := d.globalSem // 全局信号量，跨所有页面共享
	var wg sync.WaitGroup
	startTime := time.Now()

	for _, res := range allResources {
		wg.Add(1)
		go func(res models.ResourceReference) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					log.Printf("PANIC recovered in resource goroutine for %s: %v", res.URL, r)
					resultsCh <- resourceResult{res: res, err: fmt.Errorf("panic: %v", r)}
				}
			}()

			sem <- struct{}{}
			defer func() { <-sem }()

			resourceID, filePath, data, err := d.ProcessResource(res.URL, res.Type, res.Content, req.URL, req.Headers)
			if err != nil {
				resultsCh <- resourceResult{res: res, err: err}
				return
			}

			resultsCh <- resourceResult{
				res:        res,
				resourceID: resourceID,
				data:       data,
				filePath:   filePath,
			}
		}(res)
	}

	go func() {
		wg.Wait()
		close(resultsCh)
	}()

	// 收集结果
	type cssWork struct {
		cssContent string
		cssURL     string
		filePath   string
	}
	var cssWorkItems []cssWork

	processedCount := 0
	failedCount := 0
	lastLogTime := time.Now()

	for result := range resultsCh {
		processedCount++

		// 每处理 50 个资源或每 10 秒输出一次进度
		if processedCount%50 == 0 || time.Since(lastLogTime) > 10*time.Second {
			log.Printf("Resource processing progress: %d/%d completed (failed: %d)", processedCount, len(allResources), failedCount)
			lastLogTime = time.Now()
		}

		if result.err != nil {
			failedCount++
			log.Printf("Failed to process resource %s: %v", result.res.URL, result.err)
			continue
		}

		resourceIDs = append(resourceIDs, result.resourceID)
		rewriter.AddMapping(result.res.URL, result.filePath)

		// 收集需要处理的 CSS 文件
		if result.res.Type == "css" {
			cssData := result.data
			// 大文件流式落盘后 data 为 nil，需要从磁盘读回以提取子资源
			if cssData == nil && result.filePath != "" {
				if fileData, readErr := d.storage.ReadResource(result.filePath); readErr == nil {
					cssData = fileData
				} else {
					log.Printf("Failed to read CSS file for sub-resource extraction: %s: %v", result.filePath, readErr)
				}
			}
			if cssData != nil {
				cssWorkItems = append(cssWorkItems, cssWork{
					cssContent: string(cssData),
					cssURL:     result.res.URL,
					filePath:   result.filePath,
				})
			}
		}
	}

	log.Printf("Resource processing completed: %d succeeded, %d failed, took %v", len(resourceIDs), failedCount, time.Since(startTime))

	// 处理 CSS 中引用的资源（收集所有子资源后并行处理）
	type cssSubResource struct {
		absoluteURL string
		rawURL      string
		cssURL      string
	}
	var allCSSSubResources []cssSubResource

	for _, cw := range cssWorkItems {
		cssResources := d.cssParser.ExtractResources(cw.cssContent)
		for _, cssResURL := range cssResources {
			absoluteURL := d.resolveURL(cw.cssURL, cssResURL)
			allCSSSubResources = append(allCSSSubResources, cssSubResource{
				absoluteURL: absoluteURL,
				rawURL:      cssResURL,
				cssURL:      cw.cssURL,
			})
		}
	}

	// 并行处理 CSS 子资源
	if len(allCSSSubResources) > 0 {
		type cssSubResult struct {
			sub      cssSubResource
			resID    int64
			filePath string
			err      error
		}

		subResultsCh := make(chan cssSubResult, len(allCSSSubResources))
		var subWg sync.WaitGroup

		for _, sub := range allCSSSubResources {
			subWg.Add(1)
			go func(sub cssSubResource) {
				defer subWg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				subResourceID, subFilePath, _, err := d.ProcessResource(sub.absoluteURL, d.guessResourceType(sub.absoluteURL), "", req.URL, req.Headers)
				if err != nil {
					subResultsCh <- cssSubResult{sub: sub, err: err}
					return
				}

				subResultsCh <- cssSubResult{
					sub:      sub,
					resID:    subResourceID,
					filePath: subFilePath,
				}
			}(sub)
		}

		go func() {
			subWg.Wait()
			close(subResultsCh)
		}()

		for result := range subResultsCh {
			if result.err != nil {
				log.Printf("Failed to process CSS resource %s: %v", result.sub.absoluteURL, result.err)
				continue
			}

			resourceIDs = append(resourceIDs, result.resID)
			cssURLMapping[result.sub.rawURL] = result.filePath
			cssURLMapping[result.sub.absoluteURL] = result.filePath
			rewriter.AddMapping(result.sub.absoluteURL, result.filePath)
		}
	}

	// 重写 CSS 文件中的 URL
	for _, cw := range cssWorkItems {
		cssResources := d.cssParser.ExtractResources(cw.cssContent)
		if len(cssResources) > 0 {
			rewrittenCSS := d.cssParser.RewriteCSS(cw.cssContent, cssURLMapping)
			if err := d.storage.UpdateResource(cw.filePath, []byte(rewrittenCSS)); err != nil {
				log.Printf("Failed to update CSS file: %v", err)
			}
		}
	}

	// 重写 HTML 中的资源 URL
	// 1. 规范化 ../ 路径  2. 解析相对路径为绝对 URL  3. 替换为归档路径
	normalizedHTML := ResolveRelativeURLs(NormalizeHTMLURLs(req.HTML), req.URL)
	rewrittenHTML := rewriter.RewriteHTML(normalizedHTML)

	// 更新保存的 HTML 文件（用重写后的内容替换临时内容）
	if err := d.storage.UpdateHTML(tempHTMLPath, rewrittenHTML); err != nil {
		return 0, "", fmt.Errorf("update html failed: %w", err)
	}

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

	// 2. 计算新内容哈希
	htmlHash := sha256.Sum256([]byte(req.HTML))
	newContentHash := hex.EncodeToString(htmlHash[:])

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

	// 5. 处理新内容（复用 ProcessCapture 的资源处理逻辑）
	extractStart := time.Now()
	htmlResources := d.htmlExtractor.ExtractResources(req.HTML, req.URL)
	allResources := make([]models.ResourceReference, 0, len(htmlResources))
	for _, res := range htmlResources {
		allResources = append(allResources, models.ResourceReference{
			URL:  res.URL,
			Type: res.Type,
		})
	}

	log.Printf("[Update] Extracted %d resources in %v", len(allResources), time.Since(extractStart))

	// 保存新 HTML
	tempHTMLPath, err := d.storage.SaveHTML(req.URL, req.HTML, capturedAt)
	if err != nil {
		return "", fmt.Errorf("save html failed: %w", err)
	}

	// 提取正文纯文本并保存
	bodyText := ExtractBodyText(req.HTML)
	if bodyText != "" {
		if err := d.db.UpdatePageBodyText(pageID, bodyText); err != nil {
			log.Printf("Failed to save body text for page %d: %v", pageID, err)
		}
	}

	// 生成时间戳用于资源路径
	timestamp := capturedAt.Format("20060102150405")

	// 创建 URL 重写器
	rewriter := NewURLRewriter()
	rewriter.SetPageID(pageID)
	rewriter.SetTimestamp(timestamp)
	rewriter.SetBaseURL(req.URL)

	var resourceIDs []int64
	cssURLMapping := make(map[string]string)
	processStart := time.Now()

	// 并行处理资源
	type resourceResult struct {
		res        models.ResourceReference
		resourceID int64
		data       []byte
		filePath   string
		err        error
	}

	resultsCh := make(chan resourceResult, len(allResources))
	sem := d.globalSem // 全局信号量，跨所有页面共享
	var wg sync.WaitGroup

	for _, res := range allResources {
		wg.Add(1)
		go func(res models.ResourceReference) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[Update] PANIC recovered in resource goroutine for %s: %v", res.URL, r)
					resultsCh <- resourceResult{res: res, err: fmt.Errorf("panic: %v", r)}
				}
			}()

			sem <- struct{}{}
			defer func() { <-sem }()

			resourceID, filePath, data, err := d.ProcessResource(res.URL, res.Type, res.Content, req.URL, req.Headers)
			if err != nil {
				resultsCh <- resourceResult{res: res, err: err}
				return
			}

			resultsCh <- resourceResult{
				res:        res,
				resourceID: resourceID,
				data:       data,
				filePath:   filePath,
			}
		}(res)
	}

	go func() {
		wg.Wait()
		close(resultsCh)
	}()

	type cssWork struct {
		cssContent string
		cssURL     string
		filePath   string
	}
	var cssWorkItems []cssWork

	updateProcessed := 0
	updateFailed := 0
	for result := range resultsCh {
		updateProcessed++
		if result.err != nil {
			updateFailed++
			log.Printf("[Update] Failed to process resource %s: %v", result.res.URL, result.err)
			continue
		}

		resourceIDs = append(resourceIDs, result.resourceID)
		rewriter.AddMapping(result.res.URL, result.filePath)

		if result.res.Type == "css" {
			cssData := result.data
			if cssData == nil && result.filePath != "" {
				if fileData, readErr := d.storage.ReadResource(result.filePath); readErr == nil {
					cssData = fileData
				} else {
					log.Printf("[Update] Failed to read CSS file for sub-resource extraction: %s: %v", result.filePath, readErr)
				}
			}
			if cssData != nil {
				cssWorkItems = append(cssWorkItems, cssWork{
					cssContent: string(cssData),
					cssURL:     result.res.URL,
					filePath:   result.filePath,
				})
			}
		}
	}
	log.Printf("[Update] Processed %d resources (%d failed) in %v", len(resourceIDs), updateFailed, time.Since(processStart))

	// 处理 CSS 中引用的资源
	type cssSubResource struct {
		absoluteURL string
		rawURL      string
		cssURL      string
	}
	var allCSSSubResources []cssSubResource

	for _, cw := range cssWorkItems {
		cssResources := d.cssParser.ExtractResources(cw.cssContent)
		for _, cssResURL := range cssResources {
			absoluteURL := d.resolveURL(cw.cssURL, cssResURL)
			allCSSSubResources = append(allCSSSubResources, cssSubResource{
				absoluteURL: absoluteURL,
				rawURL:      cssResURL,
				cssURL:      cw.cssURL,
			})
		}
	}

	if len(allCSSSubResources) > 0 {
		cssSubStart := time.Now()
		log.Printf("[Update] Processing %d CSS sub-resources", len(allCSSSubResources))
		type cssSubResult struct {
			sub      cssSubResource
			resID    int64
			filePath string
			err      error
		}

		subResultsCh := make(chan cssSubResult, len(allCSSSubResources))
		var subWg sync.WaitGroup

		for _, sub := range allCSSSubResources {
			subWg.Add(1)
			go func(sub cssSubResource) {
				defer subWg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				subResourceID, subFilePath, _, err := d.ProcessResource(sub.absoluteURL, d.guessResourceType(sub.absoluteURL), "", req.URL, req.Headers)
				if err != nil {
					subResultsCh <- cssSubResult{sub: sub, err: err}
					return
				}

				subResultsCh <- cssSubResult{
					sub:      sub,
					resID:    subResourceID,
					filePath: subFilePath,
				}
			}(sub)
		}

		go func() {
			subWg.Wait()
			close(subResultsCh)
		}()

		for result := range subResultsCh {
			if result.err != nil {
				log.Printf("[Update] Failed to process CSS resource %s: %v", result.sub.absoluteURL, result.err)
				continue
			}

			resourceIDs = append(resourceIDs, result.resID)
			cssURLMapping[result.sub.rawURL] = result.filePath
			cssURLMapping[result.sub.absoluteURL] = result.filePath
			rewriter.AddMapping(result.sub.absoluteURL, result.filePath)
		}
		log.Printf("[Update] CSS sub-resources processed in %v", time.Since(cssSubStart))
	}

	// 重写 CSS 文件中的 URL
	for _, cw := range cssWorkItems {
		cssResources := d.cssParser.ExtractResources(cw.cssContent)
		if len(cssResources) > 0 {
			rewrittenCSS := d.cssParser.RewriteCSS(cw.cssContent, cssURLMapping)
			if err := d.storage.UpdateResource(cw.filePath, []byte(rewrittenCSS)); err != nil {
				log.Printf("[Update] Failed to update CSS file: %v", err)
			}
		}
	}

	// 重写 HTML 中的资源 URL
	normalizedHTML := ResolveRelativeURLs(NormalizeHTMLURLs(req.HTML), req.URL)
	rewrittenHTML := rewriter.RewriteHTML(normalizedHTML)

	// 更新保存的 HTML 文件
	if err := d.storage.UpdateHTML(tempHTMLPath, rewrittenHTML); err != nil {
		return "", fmt.Errorf("update html failed: %w", err)
	}

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

