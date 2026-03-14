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
	"strings"
	"time"
)

const (
	// 最大资源下载大小：50MB
	maxResourceSize = 50 * 1024 * 1024
)

type FileStorage struct {
	baseDir    string
	httpClient *http.Client
}

func NewFileStorage(baseDir string) *FileStorage {
	// 创建 HTTP 客户端，支持代理
	transport := &http.Transport{}

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

	client := &http.Client{
		Timeout:   30 * time.Second,
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

	// 解析 IP 地址
	ips, err := net.LookupIP(host)
	if err != nil {
		// DNS 解析失败，允许继续（可能是临时网络问题）
		return nil
	}

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
func (fs *FileStorage) DownloadResource(resourceURL string, pageURL string, headers map[string]string) ([]byte, string, error) {
	// 防止 SSRF 攻击：拒绝内网地址和云元数据服务
	if err := validateResourceURL(resourceURL); err != nil {
		return nil, "", fmt.Errorf("invalid resource URL: %w", err)
	}

	req, err := http.NewRequest("GET", resourceURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("create request failed: %w", err)
	}

	// 设置 User-Agent
	if ua, ok := headers["user-agent"]; ok && ua != "" {
		req.Header.Set("User-Agent", ua)
	}

	// 设置 Referer
	if pageURL != "" {
		req.Header.Set("Referer", pageURL)
	}

	// 仅在同根域名时转发 Cookie，防止泄露给第三方
	if cookie, ok := headers["cookie"]; ok && cookie != "" && isSameRootDomain(resourceURL, pageURL) {
		req.Header.Set("Cookie", cookie)
	}

	resp, err := fs.httpClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("failed to download: status %d", resp.StatusCode)
	}

	// 检查 Content-Length，防止下载超大文件
	if resp.ContentLength > maxResourceSize {
		return nil, "", fmt.Errorf("resource too large: %d bytes (max: %d)", resp.ContentLength, maxResourceSize)
	}

	// 使用 LimitReader 限制读取大小，防止恶意响应
	limitedReader := io.LimitReader(resp.Body, maxResourceSize+1)
	data, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, "", err
	}

	// 检查实际读取的大小
	if int64(len(data)) > maxResourceSize {
		return nil, "", fmt.Errorf("resource exceeds size limit: %d bytes (max: %d)", len(data), maxResourceSize)
	}

	// 计算 SHA-256 哈希
	hash := sha256.Sum256(data)
	hashStr := hex.EncodeToString(hash[:])

	return data, hashStr, nil
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

// getRootDomain extracts the root domain using public suffix list logic
// e.g. "kp.m-team.cc" -> "m-team.cc", "img.example.co.uk" -> "example.co.uk"
func getRootDomain(hostname string) string {
	// 使用简化的公共后缀列表（常见的多段 TLD）
	multiSegmentTLDs := map[string]bool{
		"co.uk": true, "co.jp": true, "co.kr": true, "co.nz": true, "co.za": true,
		"com.au": true, "com.br": true, "com.cn": true, "com.hk": true, "com.tw": true,
		"net.au": true, "org.uk": true, "gov.uk": true, "ac.uk": true,
		"ne.jp": true, "or.jp": true, "go.jp": true,
	}

	parts := strings.Split(hostname, ".")
	if len(parts) <= 1 {
		return hostname
	}

	// 检查是否为多段 TLD（如 co.uk）
	if len(parts) >= 3 {
		twoSegmentSuffix := strings.Join(parts[len(parts)-2:], ".")
		if multiSegmentTLDs[twoSegmentSuffix] {
			// 返回 domain + TLD（如 example.co.uk）
			return strings.Join(parts[len(parts)-3:], ".")
		}
	}

	// 默认返回最后两段（如 example.com）
	if len(parts) >= 2 {
		return strings.Join(parts[len(parts)-2:], ".")
	}

	return hostname
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
	default:
		return ".bin"
	}
}

// ReadHTML reads an HTML file content
func (fs *FileStorage) ReadHTML(relPath string) ([]byte, error) {
	filePath := filepath.Join(fs.baseDir, relPath)
	return os.ReadFile(filePath)
}


