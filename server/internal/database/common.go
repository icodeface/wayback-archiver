package database

import (
	"net/url"
	"strings"

	"wayback/internal/models"
)

// pageSelectColumns 定义查询页面时的标准列列表
const pageSelectColumns = "id, url, title, captured_at, html_path, content_hash, snapshot_state, first_visited, last_visited"

const resourceSelectColumns = "id, url, content_hash, resource_type, file_path, file_size, first_seen, last_seen, is_quarantined, quarantine_reason"

// pageScanner 定义可以扫描数据库行的接口
type pageScanner interface {
	Scan(dest ...any) error
}

type resourceScanner interface {
	Scan(dest ...any) error
}

// scanPage 从数据库行扫描页面数据到 Page 结构体
func scanPage(scanner pageScanner, page *models.Page) error {
	return scanner.Scan(&page.ID, &page.URL, &page.Title, &page.CapturedAt, &page.HTMLPath, &page.ContentHash, &page.SnapshotState, &page.FirstVisited, &page.LastVisited)
}

func scanResource(scanner resourceScanner, resource *models.Resource) error {
	return scanner.Scan(
		&resource.ID,
		&resource.URL,
		&resource.ContentHash,
		&resource.ResourceType,
		&resource.FilePath,
		&resource.FileSize,
		&resource.FirstSeen,
		&resource.LastSeen,
		&resource.IsQuarantined,
		&resource.QuarantineReason,
	)
}

// extractDomain 从 URL 字符串中提取主机名
func extractDomain(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Hostname()
}

// escapeLikePattern 转义 SQL LIKE 模式中的特殊字符
func escapeLikePattern(value string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		`%`, `\%`,
		`_`, `\_`,
	)
	return replacer.Replace(value)
}
