package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/publicsuffix"
	"wayback/internal/models"
)

const (
	// 最大资源下载大小：200MB
	maxResourceSize = 200 * 1024 * 1024
)

type FileStorage struct {
	baseDir    string
	httpClient *http.Client
}

type downloadMetadata struct {
	etag         string
	lastMod      string
	freshUntil   time.Time
	hasFreshness bool
	notModified  bool
}

type downloadTrace struct {
	validate    time.Duration
	request     time.Duration
	body        time.Duration
	mode        string
	statusCode  int
	contentSize int64
}

var (
	lookupIPFunc = net.LookupIP
)

func NewFileStorage(baseDir string, downloadTimeout ...int) *FileStorage {
	// 创建 HTTP 客户端，支持代理
	transport := &http.Transport{
		MaxIdleConns:        100,              // 全局最大空闲连接数
		MaxIdleConnsPerHost: 10,               // 每主机最大空闲连接数（默认 2 太小）
		MaxConnsPerHost:     20,               // 每主机最大连接数，防止对单站点创建过多连接
		IdleConnTimeout:     90 * time.Second, // 空闲连接超时回收
	}

	// 检查是否设置了代理环境变量
	if proxyURL := os.Getenv("https_proxy"); proxyURL != "" {
		if proxy, err := url.Parse(proxyURL); err == nil {
			transport.Proxy = http.ProxyURL(proxy)
		}
	} else if proxyURL := os.Getenv("http_proxy"); proxyURL != "" {
		if proxy, err := url.Parse(proxyURL); err == nil {
			transport.Proxy = http.ProxyURL(proxy)
		}
	}

	timeout := 30
	if len(downloadTimeout) > 0 && downloadTimeout[0] > 0 {
		timeout = downloadTimeout[0]
	}

	client := &http.Client{
		Timeout:   time.Duration(timeout) * time.Second,
		Transport: transport,
	}

	return &FileStorage{
		baseDir:    baseDir,
		httpClient: client,
	}
}

// validateResourceURL 验证资源 URL，防止 SSRF 攻击
func validateResourceURL(resourceURL string) error {
	parsed, err := url.Parse(resourceURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// 只允许 http 和 https 协议
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("only http and https schemes are allowed")
	}

	// 解析主机名和端口
	host := parsed.Hostname()
	if host == "" {
		return errors.New("missing hostname")
	}

	if ip := net.ParseIP(host); ip != nil {
		return validateResolvedIPs([]net.IP{ip})
	}

	// 解析 IP 地址
	ips, err := lookupIPFunc(host)
	if err != nil {
		// DNS 解析失败，允许继续（可能是临时网络问题）
		return nil
	}

	return validateResolvedIPs(ips)
}

func validateResolvedIPs(ips []net.IP) error {

	// 检查是否为内网地址或云元数据服务
	for _, ip := range ips {
		if isPrivateIP(ip) {
			return fmt.Errorf("private IP address not allowed: %s", ip.String())
		}
		if isCloudMetadataIP(ip) {
			return fmt.Errorf("cloud metadata service not allowed: %s", ip.String())
		}
	}

	return nil
}

// isPrivateIP 检查是否为私有 IP 地址
func isPrivateIP(ip net.IP) bool {
	// 将 IPv4-mapped IPv6 地址转换为 IPv4（如 ::ffff:8.8.8.8 -> 8.8.8.8）
	if ip4 := ip.To4(); ip4 != nil {
		ip = ip4
	}

	// IPv4 私有地址段
	privateIPv4Blocks := []string{
		"10.0.0.0/8",     // RFC1918
		"172.16.0.0/12",  // RFC1918
		"192.168.0.0/16", // RFC1918
		"127.0.0.0/8",    // Loopback
		"169.254.0.0/16", // Link-local
	}

	// IPv6 私有地址段
	privateIPv6Blocks := []string{
		"::1/128",   // Loopback
		"fc00::/7",  // Unique local address
		"fe80::/10", // Link-local
	}

	allBlocks := append(privateIPv4Blocks, privateIPv6Blocks...)

	for _, block := range allBlocks {
		_, subnet, err := net.ParseCIDR(block)
		if err != nil {
			continue
		}
		if subnet.Contains(ip) {
			return true
		}
	}

	return false
}

// isCloudMetadataIP 检查是否为云服务元数据 IP
func isCloudMetadataIP(ip net.IP) bool {
	// AWS/Azure/GCP 元数据服务
	metadataIPs := []string{
		"169.254.169.254/32", // AWS, Azure, GCP
		"fd00:ec2::254/128",  // AWS IPv6
	}

	for _, block := range metadataIPs {
		_, subnet, err := net.ParseCIDR(block)
		if err != nil {
			continue
		}
		if subnet.Contains(ip) {
			return true
		}
	}

	return false
}

// SaveHTML 保存 HTML 文件，按日期组织目录
func (fs *FileStorage) SaveHTML(url, html string, timestamp time.Time) (string, error) {
	// 创建日期目录：data/html/2026/03/09/
	dateDir := filepath.Join(fs.baseDir, "html", timestamp.Format("2006/01/02"))
	if err := os.MkdirAll(dateDir, 0755); err != nil {
		return "", err
	}

	// 文件名：timestamp_hash.html
	hash := sha256.Sum256([]byte(url + timestamp.String()))
	filename := fmt.Sprintf("%d_%s.html", timestamp.Unix(), hex.EncodeToString(hash[:])[:16])
	filePath := filepath.Join(dateDir, filename)

	// 写入文件
	if err := os.WriteFile(filePath, []byte(html), 0644); err != nil {
		return "", err
	}

	// 返回相对路径
	relPath, _ := filepath.Rel(fs.baseDir, filePath)
	return relPath, nil
}

// DownloadResource 下载资源并计算哈希，支持可选的认证 headers
// 小于 streamThreshold 的资源读入内存返回 data；大于的流式写入临时文件返回 tmpPath
func (fs *FileStorage) DownloadResource(resourceURL string, pageURL string, headers map[string]string, cookies []models.CaptureCookie, streamThreshold int64) (data []byte, hash string, tmpPath string, err error) {
	data, hash, tmpPath, _, _, err = fs.DownloadResourceWithMetadata(resourceURL, pageURL, headers, cookies, streamThreshold, "", "")
	return data, hash, tmpPath, err
}

// DownloadResourceWithMetadata downloads a resource and returns cache validators/freshness metadata.
func (fs *FileStorage) DownloadResourceWithMetadata(resourceURL string, pageURL string, headers map[string]string, cookies []models.CaptureCookie, streamThreshold int64, ifNoneMatch string, ifModifiedSince string) (data []byte, hash string, tmpPath string, metadata downloadMetadata, trace downloadTrace, err error) {
	validateStart := time.Now()
	// 防止 SSRF 攻击：拒绝内网地址和云元数据服务
	if err := validateResourceURL(resourceURL); err != nil {
		trace.validate = time.Since(validateStart)
		return nil, "", "", downloadMetadata{}, trace, fmt.Errorf("invalid resource URL: %w", err)
	}
	trace.validate = time.Since(validateStart)

	req, err := http.NewRequest("GET", resourceURL, nil)
	if err != nil {
		return nil, "", "", downloadMetadata{}, trace, fmt.Errorf("create request failed: %w", err)
	}

	// 设置 User-Agent
	if ua, ok := headers["user-agent"]; ok && ua != "" {
		req.Header.Set("User-Agent", ua)
	}

	// 设置 Referer
	if pageURL != "" {
		req.Header.Set("Referer", pageURL)
	}
	if ifNoneMatch != "" {
		req.Header.Set("If-None-Match", ifNoneMatch)
	}
	if ifModifiedSince != "" {
		req.Header.Set("If-Modified-Since", ifModifiedSince)
	}

	if cookie := buildCookieHeader(resourceURL, pageURL, cookies); cookie != "" {
		req.Header.Set("Cookie", cookie)
	}

	requestStart := time.Now()
	resp, err := fs.httpClient.Do(req)
	trace.request = time.Since(requestStart)
	if err != nil {
		return nil, "", "", downloadMetadata{}, trace, err
	}
	defer resp.Body.Close()
	trace.statusCode = resp.StatusCode

	metadata = downloadMetadata{
		etag:    resp.Header.Get("ETag"),
		lastMod: resp.Header.Get("Last-Modified"),
	}
	metadata.freshUntil, metadata.hasFreshness = parseFreshUntil(resp.Header, time.Now())

	if resp.StatusCode == http.StatusNotModified {
		trace.mode = "revalidate"
		metadata.notModified = true
		return nil, "", "", metadata, trace, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, "", "", metadata, trace, fmt.Errorf("failed to download: status %d", resp.StatusCode)
	}

	// 检查 Content-Length，防止下载超大文件
	if resp.ContentLength > maxResourceSize {
		return nil, "", "", metadata, trace, fmt.Errorf("resource too large: %d bytes (max: %d)", resp.ContentLength, maxResourceSize)
	}

	// 使用 LimitReader 限制读取大小
	limitedReader := io.LimitReader(resp.Body, maxResourceSize+1)

	// 已知大文件 或 阈值为 0（全部落盘）：直接流式写磁盘，内存占用 ≈ 0
	if resp.ContentLength > streamThreshold || streamThreshold <= 0 {
		bodyStart := time.Now()
		data, hash, tmpPath, err = fs.downloadToFile(limitedReader)
		trace.body = time.Since(bodyStart)
		trace.mode = "stream"
		trace.contentSize = resp.ContentLength
		return data, hash, tmpPath, metadata, trace, err
	}

	// 已知小文件（Content-Length ≤ 阈值）：直接读入内存
	if resp.ContentLength >= 0 {
		bodyStart := time.Now()
		memData, readErr := io.ReadAll(limitedReader)
		trace.body = time.Since(bodyStart)
		trace.mode = "memory"
		trace.contentSize = int64(len(memData))
		if readErr != nil {
			return nil, "", "", metadata, trace, readErr
		}
		if int64(len(memData)) > maxResourceSize {
			return nil, "", "", metadata, trace, fmt.Errorf("resource exceeds size limit: %d bytes (max: %d)", len(memData), maxResourceSize)
		}
		hashBytes := sha256.Sum256(memData)
		return memData, hex.EncodeToString(hashBytes[:]), "", metadata, trace, nil
	}

	// Content-Length 未知：先读到内存，超过阈值后溢出到磁盘
	bodyStart := time.Now()
	data, hash, tmpPath, err = fs.downloadBuffered(limitedReader, streamThreshold)
	trace.body = time.Since(bodyStart)
	trace.mode = "buffered"
	if data != nil {
		trace.contentSize = int64(len(data))
	} else if tmpPath != "" {
		if info, statErr := os.Stat(tmpPath); statErr == nil {
			trace.contentSize = info.Size()
		}
	}
	return data, hash, tmpPath, metadata, trace, err
}

func parseFreshUntil(headers http.Header, now time.Time) (time.Time, bool) {
	cacheControl := headers.Get("Cache-Control")
	if cacheControl != "" {
		for _, directive := range strings.Split(cacheControl, ",") {
			directive = strings.TrimSpace(strings.ToLower(directive))
			switch {
			case directive == "no-store", directive == "no-cache":
				return time.Time{}, true
			case strings.HasPrefix(directive, "max-age="):
				seconds, err := strconv.Atoi(strings.TrimPrefix(directive, "max-age="))
				if err != nil || seconds <= 0 {
					return time.Time{}, true
				}
				freshUntil := now.Add(time.Duration(seconds) * time.Second)
				maxFreshUntil := now.Add(resourceCacheTTL)
				if freshUntil.After(maxFreshUntil) {
					return maxFreshUntil, true
				}
				return freshUntil, true
			}
		}
	}

	if expires := headers.Get("Expires"); expires != "" {
		expiresAt, err := http.ParseTime(expires)
		if err == nil && expiresAt.After(now) {
			maxFreshUntil := now.Add(resourceCacheTTL)
			if expiresAt.After(maxFreshUntil) {
				return maxFreshUntil, true
			}
			return expiresAt, true
		}
		return time.Time{}, true
	}

	return time.Time{}, false
}

// downloadToFile 流式下载到临时文件，边写边算哈希，内存占用 ≈ 0
func (fs *FileStorage) downloadToFile(reader io.Reader) (data []byte, hash string, tmpPath string, err error) {
	tmpDir := filepath.Join(fs.baseDir, "tmp")
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return nil, "", "", err
	}

	tmpFile, err := os.CreateTemp(tmpDir, "dl-*.tmp")
	if err != nil {
		return nil, "", "", err
	}
	defer func() {
		tmpFile.Close()
		if err != nil {
			os.Remove(tmpFile.Name())
		}
	}()

	hasher := sha256.New()
	written, err := io.Copy(tmpFile, io.TeeReader(reader, hasher))
	if err != nil {
		return nil, "", "", err
	}

	if written > maxResourceSize {
		return nil, "", "", fmt.Errorf("resource exceeds size limit: %d bytes (max: %d)", written, maxResourceSize)
	}

	hashStr := hex.EncodeToString(hasher.Sum(nil))
	return nil, hashStr, tmpFile.Name(), nil
}

// downloadBuffered 先读到内存，超过阈值后溢出到磁盘
// 小文件全程在内存中完成；大文件在超过阈值的瞬间切换到磁盘流式写入
func (fs *FileStorage) downloadBuffered(reader io.Reader, threshold int64) (data []byte, hash string, tmpPath string, err error) {
	// 先读 threshold+1 字节到内存
	buf := make([]byte, threshold+1)
	n, readErr := io.ReadFull(reader, buf)

	if readErr == io.ErrUnexpectedEOF || readErr == io.EOF {
		// 读完了，实际大小 ≤ 阈值，全部在内存中
		memData := buf[:n]
		hashBytes := sha256.Sum256(memData)
		return memData, hex.EncodeToString(hashBytes[:]), "", nil
	}
	if readErr != nil {
		return nil, "", "", readErr
	}

	// 超过阈值，切换到磁盘：把已读的 buffer 写入临时文件，再流式写入剩余数据
	tmpDir := filepath.Join(fs.baseDir, "tmp")
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return nil, "", "", err
	}

	tmpFile, tmpErr := os.CreateTemp(tmpDir, "dl-*.tmp")
	if tmpErr != nil {
		return nil, "", "", tmpErr
	}
	defer func() {
		tmpFile.Close()
		if err != nil {
			os.Remove(tmpFile.Name())
		}
	}()

	hasher := sha256.New()

	// 写入已读的 buffer
	hasher.Write(buf[:n])
	if _, wErr := tmpFile.Write(buf[:n]); wErr != nil {
		return nil, "", "", wErr
	}
	buf = nil // 释放 buffer 内存

	// 流式写入剩余数据
	written, copyErr := io.Copy(tmpFile, io.TeeReader(reader, hasher))
	if copyErr != nil {
		return nil, "", "", copyErr
	}

	totalSize := int64(n) + written
	if totalSize > maxResourceSize {
		return nil, "", "", fmt.Errorf("resource exceeds size limit: %d bytes (max: %d)", totalSize, maxResourceSize)
	}

	hashStr := hex.EncodeToString(hasher.Sum(nil))
	return nil, hashStr, tmpFile.Name(), nil
}

// isSameRootDomain checks if two URLs share the same root domain
func isSameRootDomain(url1, url2 string) bool {
	if url1 == "" || url2 == "" {
		return false
	}
	parsed1, err1 := url.Parse(url1)
	parsed2, err2 := url.Parse(url2)
	if err1 != nil || err2 != nil {
		return false
	}
	return getRootDomain(parsed1.Hostname()) == getRootDomain(parsed2.Hostname())
}

// getRootDomain extracts the registrable domain using the public suffix list.
// e.g. "kp.m-team.cc" -> "m-team.cc", "img.example.co.uk" -> "example.co.uk"
func getRootDomain(hostname string) string {
	hostname = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(hostname)), ".")
	if hostname == "" {
		return ""
	}
	if ip := net.ParseIP(hostname); ip != nil {
		return hostname
	}

	root, err := publicsuffix.EffectiveTLDPlusOne(hostname)
	if err != nil {
		return hostname
	}

	return root
}

// SaveResource 保存资源文件，按哈希组织目录
func (fs *FileStorage) SaveResource(data []byte, hash, resourceType string) (string, error) {
	// 创建目录：data/resources/ab/cd/
	dir := filepath.Join(fs.baseDir, "resources", hash[:2], hash[2:4])
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}

	// 文件名：hash.ext
	ext := getExtension(resourceType)
	filename := hash + ext
	filePath := filepath.Join(dir, filename)

	// 如果文件已存在，直接返回路径
	if _, err := os.Stat(filePath); err == nil {
		relPath, _ := filepath.Rel(fs.baseDir, filePath)
		return relPath, nil
	}

	// 写入文件
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return "", err
	}

	// 返回相对路径
	relPath, _ := filepath.Rel(fs.baseDir, filePath)
	return relPath, nil
}

// SaveResourceFromFile 将临时文件移动到资源目录（大文件零拷贝存储）
func (fs *FileStorage) SaveResourceFromFile(tmpPath, hash, resourceType string) (string, error) {
	dir := filepath.Join(fs.baseDir, "resources", hash[:2], hash[2:4])
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}

	ext := getExtension(resourceType)
	filename := hash + ext
	filePath := filepath.Join(dir, filename)

	// 如果文件已存在，删除临时文件并返回路径
	if _, err := os.Stat(filePath); err == nil {
		os.Remove(tmpPath)
		relPath, _ := filepath.Rel(fs.baseDir, filePath)
		return relPath, nil
	}

	// 先尝试 rename（同文件系统零拷贝），失败则 copy
	if err := os.Rename(tmpPath, filePath); err != nil {
		// 跨文件系统 rename 会失败，用 copy 兜底
		if copyErr := copyFile(tmpPath, filePath); copyErr != nil {
			return "", copyErr
		}
		os.Remove(tmpPath)
	}

	relPath, _ := filepath.Rel(fs.baseDir, filePath)
	return relPath, nil
}

// CleanupTmp 清理临时目录中的残留文件（进程崩溃或 OOM kill 后 defer 未执行的孤儿文件）
func (fs *FileStorage) CleanupTmp() (int, error) {
	tmpDir := filepath.Join(fs.baseDir, "tmp")
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	removed := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		os.Remove(filepath.Join(tmpDir, entry.Name()))
		removed++
	}
	return removed, nil
}

// copyFile copies src to dst
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

// UpdateResource updates an existing resource file with new content
func (fs *FileStorage) UpdateResource(relPath string, data []byte) error {
	filePath := filepath.Join(fs.baseDir, relPath)

	// 写入文件
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return err
	}

	return nil
}

// UpdateHTML updates an existing HTML file with new content
func (fs *FileStorage) UpdateHTML(relPath string, html string) error {
	filePath := filepath.Join(fs.baseDir, relPath)

	// 写入文件
	if err := os.WriteFile(filePath, []byte(html), 0644); err != nil {
		return err
	}

	return nil
}

// ReadResource reads a resource file from disk
func (fs *FileStorage) ReadResource(relPath string) ([]byte, error) {
	filePath := filepath.Join(fs.baseDir, relPath)
	return os.ReadFile(filePath)
}

func (fs *FileStorage) ResourceHash(relPath string) (string, error) {
	filePath := filepath.Join(fs.baseDir, relPath)
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func (fs *FileStorage) CreateResourceQuarantineCopy(relPath string) (string, error) {
	fullPath := filepath.Join(fs.baseDir, relPath)
	if _, err := os.Stat(fullPath); err != nil {
		return "", err
	}

	ext := filepath.Ext(relPath)
	baseName := strings.TrimSuffix(filepath.Base(relPath), ext)
	resourceSubdir := strings.TrimPrefix(filepath.Dir(relPath), "resources"+string(filepath.Separator))
	quarantineRelPath := filepath.Join(
		"resources",
		"quarantine",
		resourceSubdir,
		fmt.Sprintf("%s-quarantined-%d%s", baseName, time.Now().UTC().UnixNano(), ext),
	)
	quarantineFullPath := filepath.Join(fs.baseDir, quarantineRelPath)
	if err := os.MkdirAll(filepath.Dir(quarantineFullPath), 0755); err != nil {
		return "", err
	}
	if err := copyFile(fullPath, quarantineFullPath); err != nil {
		return "", err
	}
	return quarantineRelPath, nil
}

// DeleteHTML deletes an HTML file from disk.
func (fs *FileStorage) DeleteHTML(relPath string) error {
	filePath := filepath.Join(fs.baseDir, relPath)
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func getExtension(resourceType string) string {
	switch resourceType {
	case "image":
		return ".img"
	case "css":
		return ".css"
	case "js":
		return ".js"
	case "font":
		return ".font"
	case "html":
		return ".html"
	default:
		return ".bin"
	}
}

// ReadHTML reads an HTML file content
func (fs *FileStorage) ReadHTML(relPath string) ([]byte, error) {
	filePath := filepath.Join(fs.baseDir, relPath)
	return os.ReadFile(filePath)
}
