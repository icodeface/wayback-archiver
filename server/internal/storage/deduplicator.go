package storage

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log"
	"net/url"
	"strings"
	"sync"
	"time"

	"wayback/internal/database"
	"wayback/internal/models"
)

const (
	resourceCacheTTL       = 2 * time.Hour
	resourceProcessWorkers = 8
)

type resourceCacheEntry struct {
	resourceID int64
	data       []byte
	cachedAt   time.Time
}

type Deduplicator struct {
	db            *database.DB
	storage       *FileStorage
	cssParser     *CSSParser
	htmlExtractor *HTMLResourceExtractor
	cache         sync.Map // url -> *resourceCacheEntry
}

func NewDeduplicator(db *database.DB, storage *FileStorage) *Deduplicator {
	return &Deduplicator{
		db:            db,
		storage:       storage,
		cssParser:     NewCSSParser(),
		htmlExtractor: NewHTMLResourceExtractor(),
	}
}

// ProcessResource 处理单个资源：下载、去重、存储
func (d *Deduplicator) ProcessResource(url, resourceType, base64Content string, pageURL string, headers map[string]string) (int64, []byte, error) {
	// 检查缓存（仅当没有提供 base64 内容时）
	if base64Content == "" {
		if entry, ok := d.cache.Load(url); ok {
			cached := entry.(*resourceCacheEntry)
			if time.Since(cached.cachedAt) < resourceCacheTTL {
				// 缓存命中仍需更新 last_seen
				if err := d.db.UpdateResourceLastSeen(cached.resourceID); err != nil {
					log.Printf("Failed to update last_seen for cached resource: %v", err)
				}
				return cached.resourceID, cached.data, nil
			}
			// 缓存过期，删除
			d.cache.Delete(url)
		}
	}

	var data []byte
	var hash string
	var err error

	// 如果提供了 base64 内容，直接使用
	if base64Content != "" {
		data, err = base64.StdEncoding.DecodeString(base64Content)
		if err != nil {
			return 0, nil, fmt.Errorf("base64 decode failed: %w", err)
		}
		// 计算哈希
		hashBytes := sha256.Sum256(data)
		hash = hex.EncodeToString(hashBytes[:])
	} else {
		// 否则下载资源并计算哈希
		data, hash, err = d.storage.DownloadResource(url, pageURL, headers)
		if err != nil {
			log.Printf("Download failed for %s: %v, trying fallback", url, err)
			// 兜底1：查找最近一次成功下载的相同 URL 资源
			existing, dbErr := d.db.GetResourceByURL(url)
			// 兜底2：如果精确 URL 找不到，按路径匹配（忽略查询参数差异）
			// 场景：HTML 中引用 /assets/combo.css?t=177346860，但 DB 中存的是 ?t=1773462600
			if (dbErr != nil || existing == nil) && strings.Contains(url, "?") {
				urlPath := url[:strings.IndexByte(url, '?')]
				existing, dbErr = d.db.GetResourceByURLLike(urlPath + "%")
				if existing != nil {
					log.Printf("Fallback: found resource by URL path match: %s -> %s", url, existing.URL)
				}
			}
			if dbErr != nil || existing == nil {
				return 0, nil, fmt.Errorf("download failed and no fallback: %w", err)
			}
			// 读取已存储的文件内容
			fileData, readErr := d.storage.ReadResource(existing.FilePath)
			if readErr != nil {
				return 0, nil, fmt.Errorf("download failed and fallback read failed: %w", err)
			}
			log.Printf("Fallback: reusing previous resource (ID: %d) for: %s", existing.ID, url)
			if updateErr := d.db.UpdateResourceLastSeen(existing.ID); updateErr != nil {
				log.Printf("Failed to update last_seen for fallback resource: %v", updateErr)
			}
			d.cache.Store(url, &resourceCacheEntry{resourceID: existing.ID, data: fileData, cachedAt: time.Now()})
			return existing.ID, fileData, nil
		}
	}

	// 检查是否已有相同 URL 的资源记录
	existingByURL, err := d.db.GetResourceByURL(url)
	if err != nil {
		return 0, nil, fmt.Errorf("db query by url failed: %w", err)
	}

	if existingByURL != nil {
		// 同 URL 已存在，更新最后见到时间
		if err := d.db.UpdateResourceLastSeen(existingByURL.ID); err != nil {
			return 0, nil, err
		}
		d.cache.Store(url, &resourceCacheEntry{resourceID: existingByURL.ID, data: data, cachedAt: time.Now()})
		return existingByURL.ID, data, nil
	}

	// 检查是否有相同哈希的资源（不同 URL，内容相同）
	existingByHash, err := d.db.GetResourceByHash(hash)
	if err != nil {
		return 0, nil, fmt.Errorf("db query by hash failed: %w", err)
	}

	var filePath string
	if existingByHash != nil {
		// 内容相同但 URL 不同，复用文件，创建新 DB 记录
		filePath = existingByHash.FilePath
		log.Printf("Same content (hash: %s) different URL, reusing file: %s", hash[:16], url)
	} else {
		// 全新资源，保存文件
		filePath, err = d.storage.SaveResource(data, hash, resourceType)
		if err != nil {
			return 0, nil, fmt.Errorf("save failed: %w", err)
		}
	}

	// 创建数据库记录（使用 ON CONFLICT 防止竞态）
	resourceID, err := d.db.CreateResourceIfNotExists(url, hash, resourceType, filePath, int64(len(data)))
	if err != nil {
		return 0, nil, fmt.Errorf("db insert failed: %w", err)
	}

	log.Printf("New resource record (hash: %s): %s", hash[:16], url)
	d.cache.Store(url, &resourceCacheEntry{resourceID: resourceID, data: data, cachedAt: time.Now()})
	return resourceID, data, nil
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
	sem := make(chan struct{}, resourceProcessWorkers)
	var wg sync.WaitGroup

	for _, res := range allResources {
		wg.Add(1)
		go func(res models.ResourceReference) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			resourceID, data, err := d.ProcessResource(res.URL, res.Type, res.Content, req.URL, req.Headers)
			if err != nil {
				resultsCh <- resourceResult{res: res, err: err}
				return
			}

			resource, err := d.db.GetResourceByID(resourceID)
			if err != nil {
				resultsCh <- resourceResult{res: res, err: fmt.Errorf("get resource info: %w", err)}
				return
			}
			if resource == nil {
				resultsCh <- resourceResult{res: res, err: fmt.Errorf("resource not found in DB (ID: %d)", resourceID)}
				return
			}

			resultsCh <- resourceResult{
				res:        res,
				resourceID: resourceID,
				data:       data,
				filePath:   resource.FilePath,
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

	for result := range resultsCh {
		if result.err != nil {
			log.Printf("Failed to process resource %s: %v", result.res.URL, result.err)
			continue
		}

		resourceIDs = append(resourceIDs, result.resourceID)
		rewriter.AddMapping(result.res.URL, result.filePath)

		// 收集需要处理的 CSS 文件
		if result.res.Type == "css" && result.data != nil {
			cssWorkItems = append(cssWorkItems, cssWork{
				cssContent: string(result.data),
				cssURL:     result.res.URL,
				filePath:   result.filePath,
			})
		}
	}

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

				subResourceID, _, err := d.ProcessResource(sub.absoluteURL, d.guessResourceType(sub.absoluteURL), "", req.URL, req.Headers)
				if err != nil {
					subResultsCh <- cssSubResult{sub: sub, err: err}
					return
				}

				subResource, err := d.db.GetResourceByID(subResourceID)
				if err != nil {
					subResultsCh <- cssSubResult{sub: sub, err: fmt.Errorf("get CSS sub-resource info: %w", err)}
					return
				}

				subResultsCh <- cssSubResult{
					sub:      sub,
					resID:    subResourceID,
					filePath: subResource.FilePath,
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

	// 重写 HTML 中的资源 URL（先规范化 HTML 中的 ../ 路径）
	normalizedHTML := NormalizeHTMLURLs(req.HTML)
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
func (d *Deduplicator) UpdateCapture(pageID int64, req *models.CaptureRequest) (string, error) {
	startTime := time.Now()
	log.Printf("[Update] Starting update for page %d", pageID)

	// 1. 获取现有页面信息
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
	oldHTMLPath := page.HTMLPath

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
	sem := make(chan struct{}, resourceProcessWorkers)
	var wg sync.WaitGroup

	for _, res := range allResources {
		wg.Add(1)
		go func(res models.ResourceReference) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			resourceID, data, err := d.ProcessResource(res.URL, res.Type, res.Content, req.URL, req.Headers)
			if err != nil {
				resultsCh <- resourceResult{res: res, err: err}
				return
			}

			resource, err := d.db.GetResourceByID(resourceID)
			if err != nil {
				resultsCh <- resourceResult{res: res, err: fmt.Errorf("get resource info: %w", err)}
				return
			}
			if resource == nil {
				resultsCh <- resourceResult{res: res, err: fmt.Errorf("resource not found in DB (ID: %d)", resourceID)}
				return
			}

			resultsCh <- resourceResult{
				res:        res,
				resourceID: resourceID,
				data:       data,
				filePath:   resource.FilePath,
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

	for result := range resultsCh {
		if result.err != nil {
			log.Printf("[Update] Failed to process resource %s: %v", result.res.URL, result.err)
			continue
		}

		resourceIDs = append(resourceIDs, result.resourceID)
		rewriter.AddMapping(result.res.URL, result.filePath)

		if result.res.Type == "css" && result.data != nil {
			cssWorkItems = append(cssWorkItems, cssWork{
				cssContent: string(result.data),
				cssURL:     result.res.URL,
				filePath:   result.filePath,
			})
		}
	}
	log.Printf("[Update] Processed %d resources in %v", len(resourceIDs), time.Since(processStart))

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

				subResourceID, _, err := d.ProcessResource(sub.absoluteURL, d.guessResourceType(sub.absoluteURL), "", req.URL, req.Headers)
				if err != nil {
					subResultsCh <- cssSubResult{sub: sub, err: err}
					return
				}

				subResource, err := d.db.GetResourceByID(subResourceID)
				if err != nil {
					subResultsCh <- cssSubResult{sub: sub, err: fmt.Errorf("get CSS sub-resource info: %w", err)}
					return
				}

				subResultsCh <- cssSubResult{
					sub:      sub,
					resID:    subResourceID,
					filePath: subResource.FilePath,
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

	// 重写 HTML 中的资源 URL（先规范化 HTML 中的 ../ 路径）
	normalizedHTML := NormalizeHTMLURLs(req.HTML)
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

	// 删除旧 HTML 文件（DB 已指向新文件，此时删除安全）
	if oldHTMLPath != tempHTMLPath {
		if err := d.storage.DeleteHTML(oldHTMLPath); err != nil {
			log.Printf("Failed to delete old HTML: %v", err)
		}
	}

	log.Printf("[Update] Page updated (ID: %d, hash: %s, %d resources, %v)", pageID, newContentHash[:16], len(resourceIDs), time.Since(startTime))
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
