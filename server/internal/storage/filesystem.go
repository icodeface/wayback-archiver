package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
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

	// 读取内容
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
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

// getRootDomain extracts the root domain (last two segments) from a hostname
// e.g. "kp.m-team.cc" -> "m-team.cc", "img.m-team.cc" -> "m-team.cc"
func getRootDomain(hostname string) string {
	parts := strings.Split(hostname, ".")
	if len(parts) <= 2 {
		return hostname
	}
	return strings.Join(parts[len(parts)-2:], ".")
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
