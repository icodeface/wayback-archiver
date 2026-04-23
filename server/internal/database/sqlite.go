package database

import (
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"wayback/internal/models"
)

//go:embed migrations/sqlite/init_db.sql
var sqliteSchema string

// SQLiteDB SQLite 数据库实现
type SQLiteDB struct {
	conn *sql.DB
	qb   *QueryBuilder
}

// NewSQLite 创建 SQLite 数据库连接
func NewSQLite(dbPath string) (Database, error) {
	// 确保数据库目录存在
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	// SQLite 连接字符串：启用外键约束、WAL 模式
	connStr := fmt.Sprintf("file:%s?_foreign_keys=on&_journal_mode=WAL", dbPath)

	conn, err := sql.Open("sqlite3", connStr)
	if err != nil {
		return nil, err
	}

	// 配置连接池（SQLite WAL 模式支持多读单写）
	conn.SetMaxOpenConns(10) // 允许多个并发读连接
	conn.SetMaxIdleConns(5)  // 保持一些空闲连接
	conn.SetConnMaxLifetime(0)

	if err := conn.Ping(); err != nil {
		return nil, err
	}

	db := &SQLiteDB{
		conn: conn,
		qb:   NewQueryBuilder(DBTypeSQLite),
	}

	// 注册自定义函数
	if err := db.registerCustomFunctions(); err != nil {
		return nil, err
	}

	// 执行 schema 初始化
	if err := db.ensureSchema(); err != nil {
		return nil, err
	}

	return db, nil
}

// registerCustomFunctions 注册 SQLite 自定义函数
func (db *SQLiteDB) registerCustomFunctions() error {
	// SQLite 的自定义函数通过 Go 驱动注册
	// extract_domain 在 ensureDomainColumn 中使用 Go 函数 extractDomain() 处理
	// 不需要在 SQL 层注册
	return nil
}

// ensureSchema 确保数据库 schema 存在
func (db *SQLiteDB) ensureSchema() error {
	// 检查 pages 表是否存在
	var count int
	err := db.conn.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='pages'").Scan(&count)
	if err != nil {
		return err
	}

	if count == 0 {
		// 执行初始化脚本
		if _, err := db.conn.Exec(sqliteSchema); err != nil {
			return fmt.Errorf("failed to initialize schema: %w", err)
		}
	}

	// 执行增量迁移
	return db.ensureMigrations()
}

// ensureMigrations 执行增量迁移
func (db *SQLiteDB) ensureMigrations() error {
	// 检查 domain 列
	if err := db.ensureDomainColumn(); err != nil {
		return err
	}

	// 检查 snapshot_state 列
	if err := db.ensureSnapshotStateColumn(); err != nil {
		return err
	}

	return nil
}

// ensureDomainColumn 确保 domain 列存在
func (db *SQLiteDB) ensureDomainColumn() error {
	// SQLite 不支持 ADD COLUMN IF NOT EXISTS，需要先检查
	var count int
	err := db.conn.QueryRow("SELECT COUNT(*) FROM pragma_table_info('pages') WHERE name='domain'").Scan(&count)
	if err != nil {
		return err
	}

	if count == 0 {
		if _, err := db.conn.Exec("ALTER TABLE pages ADD COLUMN domain TEXT DEFAULT ''"); err != nil {
			return err
		}

		// 创建索引
		if _, err := db.conn.Exec("CREATE INDEX IF NOT EXISTS idx_pages_domain ON pages (domain)"); err != nil {
			return err
		}

		// 回填数据（使用 Go 函数而非 SQL）
		rows, err := db.conn.Query("SELECT id, url FROM pages WHERE domain = ''")
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var id int64
			var rawURL string
			if err := rows.Scan(&id, &rawURL); err != nil {
				return err
			}

			domain := extractDomain(rawURL)
			if _, err := db.conn.Exec("UPDATE pages SET domain = ? WHERE id = ?", domain, id); err != nil {
				return err
			}
		}
	}

	return nil
}

// ensureSnapshotStateColumn 确保 snapshot_state 列存在
func (db *SQLiteDB) ensureSnapshotStateColumn() error {
	var count int
	err := db.conn.QueryRow("SELECT COUNT(*) FROM pragma_table_info('pages') WHERE name='snapshot_state'").Scan(&count)
	if err != nil {
		return err
	}

	if count == 0 {
		// 添加列时设置 DEFAULT 'ready'，让现有记录自动填充为 'ready'
		if _, err := db.conn.Exec("ALTER TABLE pages ADD COLUMN snapshot_state VARCHAR(16) NOT NULL DEFAULT 'ready'"); err != nil {
			return err
		}

		// 注意：新记录会在 CreatePage 中显式设置为 'pending'
		// 现有记录保持 'ready' 状态（表示已完成）
	}

	return nil
}

func (db *SQLiteDB) Close() error {
	return db.conn.Close()
}

// CreatePage 创建页面记录
func (db *SQLiteDB) CreatePage(url, title, htmlPath, contentHash string, capturedAt time.Time) (int64, error) {
	result, err := db.conn.Exec(
		"INSERT INTO pages (url, title, html_path, content_hash, snapshot_state, captured_at, first_visited, last_visited, domain) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		url, title, htmlPath, contentHash, models.SnapshotStatePending, capturedAt, capturedAt, capturedAt, extractDomain(url),
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// UpdatePageBodyText 更新页面正文文本（用于全文搜索）
func (db *SQLiteDB) UpdatePageBodyText(id int64, bodyText string) error {
	_, err := db.conn.Exec("UPDATE pages SET body_text = ? WHERE id = ?", bodyText, id)
	return err
}

// GetPageByURLAndHash 根据 URL 和内容哈希查找页面
func (db *SQLiteDB) GetPageByURLAndHash(url, contentHash string) (*models.Page, error) {
	var p models.Page
	row := db.conn.QueryRow(
		"SELECT "+pageSelectColumns+" FROM pages WHERE url = ? AND content_hash = ? ORDER BY CASE snapshot_state WHEN 'ready' THEN 0 WHEN 'pending' THEN 1 ELSE 2 END, first_visited ASC LIMIT 1",
		url, contentHash,
	)
	err := scanPage(row, &p)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// UpdatePageLastVisited 更新页面最后访问时间
func (db *SQLiteDB) UpdatePageLastVisited(id int64, lastVisited time.Time) error {
	_, err := db.conn.Exec("UPDATE pages SET last_visited = ? WHERE id = ?", lastVisited, id)
	return err
}

// GetResourceByHash 根据哈希查找资源
func (db *SQLiteDB) GetResourceByHash(hash string) (*models.Resource, error) {
	var r models.Resource
	err := db.conn.QueryRow(
		"SELECT id, url, content_hash, resource_type, file_path, file_size, first_seen, last_seen FROM resources WHERE content_hash = ?",
		hash,
	).Scan(&r.ID, &r.URL, &r.ContentHash, &r.ResourceType, &r.FilePath, &r.FileSize, &r.FirstSeen, &r.LastSeen)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// CreateResource 创建资源记录
func (db *SQLiteDB) CreateResource(url, hash, resourceType, filePath string, fileSize int64) (int64, error) {
	result, err := db.conn.Exec(
		"INSERT INTO resources (url, content_hash, resource_type, file_path, file_size) VALUES (?, ?, ?, ?, ?)",
		url, hash, resourceType, filePath, fileSize,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// UpdateResourceLastSeen 更新资源最后见到时间
func (db *SQLiteDB) UpdateResourceLastSeen(id int64) error {
	query := fmt.Sprintf("UPDATE resources SET last_seen = %s WHERE id = ?", db.qb.CurrentTimestamp())
	_, err := db.conn.Exec(query, id)
	return err
}

func (db *SQLiteDB) touchResourcesLastSeen(tx *sql.Tx, resourceIDs []int64) error {
	if len(resourceIDs) == 0 {
		return nil
	}

	// SQLite: 展开为 IN (?, ?, ...)
	placeholders := make([]string, len(resourceIDs))
	args := make([]interface{}, len(resourceIDs))
	for i, id := range resourceIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf("UPDATE resources SET last_seen = %s WHERE id IN (%s)",
		db.qb.CurrentTimestamp(), strings.Join(placeholders, ", "))
	_, err := tx.Exec(query, args...)
	return err
}

// LinkPageResource 关联页面和资源
func (db *SQLiteDB) LinkPageResource(pageID, resourceID int64) error {
	_, err := db.conn.Exec(
		"INSERT OR IGNORE INTO page_resources (page_id, resource_id) VALUES (?, ?)",
		pageID, resourceID,
	)
	return err
}

// LinkPageResources links a page to all provided resources in a single transaction.
func (db *SQLiteDB) LinkPageResources(pageID int64, resourceIDs []int64) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, resourceID := range resourceIDs {
		if _, err := tx.Exec(
			"INSERT OR IGNORE INTO page_resources (page_id, resource_id) VALUES (?, ?)",
			pageID, resourceID,
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// CheckRecentCapture 检查最近是否已捕获相同 URL（5分钟内）
func (db *SQLiteDB) CheckRecentCapture(url string, within time.Duration) (bool, error) {
	var count int
	err := db.conn.QueryRow(
		"SELECT COUNT(*) FROM pages WHERE url = ? AND captured_at > ?",
		url, time.Now().Add(-within),
	).Scan(&count)
	return count > 0, err
}

// GetResourceByID 根据 ID 获取资源
func (db *SQLiteDB) GetResourceByID(id int64) (*models.Resource, error) {
	var r models.Resource
	err := db.conn.QueryRow(
		"SELECT id, url, content_hash, resource_type, file_path, file_size, first_seen, last_seen FROM resources WHERE id = ?",
		id,
	).Scan(&r.ID, &r.URL, &r.ContentHash, &r.ResourceType, &r.FilePath, &r.FileSize, &r.FirstSeen, &r.LastSeen)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// GetResourceByURL 根据 URL 获取资源（返回最新的）
func (db *SQLiteDB) GetResourceByURL(url string) (*models.Resource, error) {
	var r models.Resource
	err := db.conn.QueryRow(
		"SELECT id, url, content_hash, resource_type, file_path, file_size, first_seen, last_seen FROM resources WHERE url = ? ORDER BY last_seen DESC LIMIT 1",
		url,
	).Scan(&r.ID, &r.URL, &r.ContentHash, &r.ResourceType, &r.FilePath, &r.FileSize, &r.FirstSeen, &r.LastSeen)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// GetResourceByURLLike 根据 URL 模糊匹配查找资源（如忽略查询参数差异）
func (db *SQLiteDB) GetResourceByURLLike(pattern string) (*models.Resource, error) {
	var r models.Resource
	err := db.conn.QueryRow(
		"SELECT id, url, content_hash, resource_type, file_path, file_size, first_seen, last_seen FROM resources WHERE url LIKE ? ORDER BY last_seen DESC LIMIT 1",
		pattern,
	).Scan(&r.ID, &r.URL, &r.ContentHash, &r.ResourceType, &r.FilePath, &r.FileSize, &r.FirstSeen, &r.LastSeen)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// ListPages 列出页面（分页，支持时间和域名过滤）
func (db *SQLiteDB) ListPages(limit, offset int, from, to *time.Time, domain string) ([]models.Page, error) {
	query := "SELECT " + pageSelectColumns + " FROM pages"
	args := []interface{}{}

	// 构建 WHERE 条件
	var conditions []string
	if from != nil {
		conditions = append(conditions, "captured_at >= ?")
		args = append(args, *from)
	}
	if to != nil {
		// to 使用 < nextDay 确保包含当天全部记录
		nextDay := to.AddDate(0, 0, 1)
		conditions = append(conditions, "captured_at < ?")
		args = append(args, nextDay)
	}
	if domain != "" {
		conditions = append(conditions, "(domain = ? OR domain LIKE ?)")
		args = append(args, domain, "%."+domain)
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	query += " ORDER BY last_visited DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	pages := []models.Page{}
	for rows.Next() {
		var p models.Page
		if err := scanPage(rows, &p); err != nil {
			return nil, err
		}
		pages = append(pages, p)
	}
	return pages, nil
}

// GetTotalPagesCount 获取页面总数（支持时间和域名过滤）
func (db *SQLiteDB) GetTotalPagesCount(from, to *time.Time, domain string) (int, error) {
	query := "SELECT COUNT(*) FROM pages"
	args := []interface{}{}

	// 构建 WHERE 条件
	var conditions []string
	if from != nil {
		conditions = append(conditions, "captured_at >= ?")
		args = append(args, *from)
	}
	if to != nil {
		// to 使用 < nextDay 确保包含当天全部记录
		nextDay := to.AddDate(0, 0, 1)
		conditions = append(conditions, "captured_at < ?")
		args = append(args, nextDay)
	}
	if domain != "" {
		conditions = append(conditions, "(domain = ? OR domain LIKE ?)")
		args = append(args, domain, "%."+domain)
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	var count int
	err := db.conn.QueryRow(query, args...).Scan(&count)
	return count, err
}

// GetPageByID 根据 ID 获取页面
func (db *SQLiteDB) GetPageByID(id string) (*models.Page, error) {
	var p models.Page
	row := db.conn.QueryRow(
		"SELECT "+pageSelectColumns+" FROM pages WHERE id = ?",
		id,
	)
	err := scanPage(row, &p)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// SearchPages 搜索页面（按 URL、标题或正文内容，支持时间和域名过滤）
func (db *SQLiteDB) SearchPages(keyword string, from, to *time.Time, domain string) ([]models.Page, error) {
	// SQLite 使用 FTS5 全文搜索
	query := `SELECT ` + pageSelectColumns + ` FROM pages WHERE id IN (
		SELECT rowid FROM pages_fts WHERE pages_fts MATCH ?
	)`
	args := []interface{}{keyword + "*"} // FTS5 前缀匹配

	// 追加时间过滤条件
	if from != nil {
		query += " AND captured_at >= ?"
		args = append(args, *from)
	}
	if to != nil {
		// to 使用 < nextDay 确保包含当天全部记录
		nextDay := to.AddDate(0, 0, 1)
		query += " AND captured_at < ?"
		args = append(args, nextDay)
	}
	if domain != "" {
		query += " AND (domain = ? OR domain LIKE ?)"
		args = append(args, domain, "%."+domain)
	}

	query += " ORDER BY last_visited DESC LIMIT 100"

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	pages := []models.Page{}
	for rows.Next() {
		var p models.Page
		if err := scanPage(rows, &p); err != nil {
			return nil, err
		}
		pages = append(pages, p)
	}
	return pages, nil
}

// GetPagesWithoutBodyText 获取所有没有 body_text 的页面（用于回填）
func (db *SQLiteDB) GetPagesWithoutBodyText() ([]models.Page, error) {
	rows, err := db.conn.Query(
		"SELECT " + pageSelectColumns + " FROM pages WHERE body_text IS NULL ORDER BY id",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	pages := []models.Page{}
	for rows.Next() {
		var p models.Page
		if err := scanPage(rows, &p); err != nil {
			return nil, err
		}
		pages = append(pages, p)
	}
	return pages, nil
}

// GetResourcesByPageID 获取页面关联的所有资源
func (db *SQLiteDB) GetResourcesByPageID(pageID int64) ([]models.Resource, error) {
	rows, err := db.conn.Query(`
		SELECT r.id, r.url, r.content_hash, r.resource_type, r.file_path, r.file_size, r.first_seen, r.last_seen
		FROM resources r
		INNER JOIN page_resources pr ON r.id = pr.resource_id
		WHERE pr.page_id = ?
	`, pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var resources []models.Resource
	for rows.Next() {
		var r models.Resource
		if err := rows.Scan(&r.ID, &r.URL, &r.ContentHash, &r.ResourceType, &r.FilePath, &r.FileSize, &r.FirstSeen, &r.LastSeen); err != nil {
			return nil, err
		}
		resources = append(resources, r)
	}
	return resources, nil
}

// GetResourceByURLAndPageID 根据URL和页面ID查找资源
func (db *SQLiteDB) GetResourceByURLAndPageID(url string, pageID int64) (*models.Resource, error) {
	r, err := db.GetLinkedResourceByURLAndPageID(url, pageID)
	if err != nil || r != nil {
		return r, err
	}

	// 如果页面关联中没有，尝试直接按URL查找最新的
	return db.GetResourceByURL(url)
}

// GetLinkedResourceByURLAndPageID 根据URL和页面ID查找资源，只查询 page_resources 关联，不做全局兜底
func (db *SQLiteDB) GetLinkedResourceByURLAndPageID(url string, pageID int64) (*models.Resource, error) {
	var r models.Resource
	err := db.conn.QueryRow(`
		SELECT r.id, r.url, r.content_hash, r.resource_type, r.file_path, r.file_size, r.first_seen, r.last_seen
		FROM resources r
		INNER JOIN page_resources pr ON r.id = pr.resource_id
		WHERE pr.page_id = ? AND r.url = ?
		LIMIT 1
	`, pageID, url).Scan(&r.ID, &r.URL, &r.ContentHash, &r.ResourceType, &r.FilePath, &r.FileSize, &r.FirstSeen, &r.LastSeen)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// GetResourceByURLPrefix 根据 URL 前缀匹配资源（处理 DB 中 URL 带 #fragment 的情况）
func (db *SQLiteDB) GetResourceByURLPrefix(urlPrefix string, pageID int64) (*models.Resource, error) {
	var r models.Resource
	escapedPrefix := escapeLikePattern(urlPrefix)
	err := db.conn.QueryRow(`
		SELECT r.id, r.url, r.content_hash, r.resource_type, r.file_path, r.file_size, r.first_seen, r.last_seen
		FROM resources r
		INNER JOIN page_resources pr ON r.id = pr.resource_id
		WHERE pr.page_id = ? AND (r.url LIKE ? ESCAPE '\' OR r.url LIKE ? ESCAPE '\')
		ORDER BY r.last_seen DESC, r.id DESC
		LIMIT 1
	`, pageID, escapedPrefix+"#%", escapedPrefix+"%23%").Scan(&r.ID, &r.URL, &r.ContentHash, &r.ResourceType, &r.FilePath, &r.FileSize, &r.FirstSeen, &r.LastSeen)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// GetResourceByURLPath 根据 URL 路径匹配资源（忽略查询参数，用于同一图片不同 token 的情况）
func (db *SQLiteDB) GetResourceByURLPath(urlPath string, pageID int64) (*models.Resource, error) {
	var r models.Resource
	escapedPath := escapeLikePattern(urlPath)
	err := db.conn.QueryRow(`
		SELECT r.id, r.url, r.content_hash, r.resource_type, r.file_path, r.file_size, r.first_seen, r.last_seen
		FROM resources r
		INNER JOIN page_resources pr ON r.id = pr.resource_id
		WHERE pr.page_id = ? AND (r.url = ? OR r.url LIKE ? ESCAPE '\')
		ORDER BY CASE WHEN r.url = ? THEN 0 ELSE 1 END, r.last_seen DESC, r.id DESC
		LIMIT 1
	`, pageID, urlPath, escapedPath+"?%", urlPath).Scan(&r.ID, &r.URL, &r.ContentHash, &r.ResourceType, &r.FilePath, &r.FileSize, &r.FirstSeen, &r.LastSeen)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// UpdatePageContent 更新页面内容（HTML路径、哈希、标题、最后访问时间）
func (db *SQLiteDB) UpdatePageContent(id int64, htmlPath, contentHash, title string) error {
	query := fmt.Sprintf("UPDATE pages SET html_path = ?, content_hash = ?, title = ?, snapshot_state = ?, last_visited = %s WHERE id = ?", db.qb.CurrentTimestamp())
	_, err := db.conn.Exec(query, htmlPath, contentHash, title, models.SnapshotStateReady, id)
	return err
}

// ReplacePageSnapshot atomically swaps the page HTML metadata and resource links.
func (db *SQLiteDB) ReplacePageSnapshot(id int64, htmlPath, contentHash, title string, bodyText *string, resourceIDs []int64) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	nowSQL := db.qb.CurrentTimestamp()
	if bodyText != nil {
		query := fmt.Sprintf("UPDATE pages SET html_path = ?, content_hash = ?, title = ?, body_text = ?, snapshot_state = ?, last_visited = %s WHERE id = ?", nowSQL)
		if _, err := tx.Exec(query, htmlPath, contentHash, title, *bodyText, models.SnapshotStateReady, id); err != nil {
			return err
		}
	} else {
		query := fmt.Sprintf("UPDATE pages SET html_path = ?, content_hash = ?, title = ?, snapshot_state = ?, last_visited = %s WHERE id = ?", nowSQL)
		if _, err := tx.Exec(query, htmlPath, contentHash, title, models.SnapshotStateReady, id); err != nil {
			return err
		}
	}

	if _, err := tx.Exec("DELETE FROM page_resources WHERE page_id = ?", id); err != nil {
		return err
	}
	if err := db.touchResourcesLastSeen(tx, resourceIDs); err != nil {
		return err
	}

	for _, resourceID := range resourceIDs {
		if _, err := tx.Exec(
			"INSERT OR IGNORE INTO page_resources (page_id, resource_id) VALUES (?, ?)",
			id, resourceID,
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (db *SQLiteDB) ResetPageForCreateRetry(id int64, title, htmlPath string, capturedAt time.Time) (string, error) {
	tx, err := db.conn.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	var oldHTMLPath string
	if err := tx.QueryRow("SELECT html_path FROM pages WHERE id = ?", id).Scan(&oldHTMLPath); err != nil {
		return "", err
	}
	if _, err := tx.Exec(
		"UPDATE pages SET title = ?, html_path = ?, snapshot_state = ?, last_visited = ? WHERE id = ?",
		title, htmlPath, models.SnapshotStatePending, capturedAt, id,
	); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return oldHTMLPath, nil
}

func (db *SQLiteDB) FinalizePageCreate(id int64, resourceIDs []int64) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("UPDATE pages SET snapshot_state = ? WHERE id = ?", models.SnapshotStateReady, id); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM page_resources WHERE page_id = ?", id); err != nil {
		return err
	}
	if err := db.touchResourcesLastSeen(tx, resourceIDs); err != nil {
		return err
	}
	for _, resourceID := range resourceIDs {
		if _, err := tx.Exec(
			"INSERT OR IGNORE INTO page_resources (page_id, resource_id) VALUES (?, ?)",
			id, resourceID,
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (db *SQLiteDB) MarkPageCreateFailed(id int64) error {
	_, err := db.conn.Exec("UPDATE pages SET snapshot_state = ? WHERE id = ?", models.SnapshotStateFailed, id)
	return err
}

// GetPagesByURL 获取同一 URL 的所有快照（按时间倒序）
func (db *SQLiteDB) GetPagesByURL(pageURL string) ([]models.Page, error) {
	rows, err := db.conn.Query(
		"SELECT "+pageSelectColumns+" FROM pages WHERE url = ? ORDER BY first_visited DESC",
		pageURL,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	pages := []models.Page{}
	for rows.Next() {
		var p models.Page
		if err := scanPage(rows, &p); err != nil {
			return nil, err
		}
		pages = append(pages, p)
	}
	return pages, nil
}

// GetSnapshotNeighbors 获取某个快照的前后快照（用于导航）
func (db *SQLiteDB) GetSnapshotNeighbors(pageURL string, currentID int64) (prev *models.Page, next *models.Page, total int, err error) {
	// 获取总数
	err = db.conn.QueryRow("SELECT COUNT(*) FROM pages WHERE url = ?", pageURL).Scan(&total)
	if err != nil {
		return
	}

	// 前一个快照（比当前更早）
	var p models.Page
	row := db.conn.QueryRow(
		"SELECT "+pageSelectColumns+" FROM pages WHERE url = ? AND first_visited < (SELECT first_visited FROM pages WHERE id = ?) ORDER BY first_visited DESC LIMIT 1",
		pageURL, currentID,
	)
	err = scanPage(row, &p)
	if err == nil {
		prev = &p
	} else if err != sql.ErrNoRows {
		return
	}
	err = nil

	// 后一个快照（比当前更新）
	var n models.Page
	row = db.conn.QueryRow(
		"SELECT "+pageSelectColumns+" FROM pages WHERE url = ? AND first_visited > (SELECT first_visited FROM pages WHERE id = ?) ORDER BY first_visited ASC LIMIT 1",
		pageURL, currentID,
	)
	err = scanPage(row, &n)
	if err == nil {
		next = &n
	} else if err != sql.ErrNoRows {
		return
	}
	err = nil
	return
}

// DeletePageResources 删除页面资源关联（不删除资源本身）
func (db *SQLiteDB) DeletePageResources(pageID int64) error {
	_, err := db.conn.Exec("DELETE FROM page_resources WHERE page_id = ?", pageID)
	return err
}

// DeletePage 删除页面记录（包括关联关系）
func (db *SQLiteDB) DeletePage(id int64) error {
	// 开启事务
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 删除页面资源关联
	_, err = tx.Exec("DELETE FROM page_resources WHERE page_id = ?", id)
	if err != nil {
		return err
	}

	// 删除页面记录
	_, err = tx.Exec("DELETE FROM pages WHERE id = ?", id)
	if err != nil {
		return err
	}

	return tx.Commit()
}
