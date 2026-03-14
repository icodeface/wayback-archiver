package database

import (
	"database/sql"
	"fmt"
	"net/url"
	"time"

	_ "github.com/lib/pq"
	"wayback/internal/models"
)

type DB struct {
	conn *sql.DB
}

func New(host, port, user, password, dbname string) (*DB, error) {
	// 构建连接字符串，确保 dbname 参数正确传递
	connStr := fmt.Sprintf("host=%s port=%s dbname=%s sslmode=disable",
		host, port, dbname)

	// 只有在 user 不为空时才添加
	if user != "" {
		connStr = fmt.Sprintf("host=%s port=%s user=%s dbname=%s sslmode=disable",
			host, port, user, dbname)
	}

	// 只有在 password 不为空时才添加
	if password != "" {
		connStr = fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
			host, port, user, password, dbname)
	}

	conn, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, err
	}

	// 配置连接池
	conn.SetMaxOpenConns(25)                 // 最大打开连接数
	conn.SetMaxIdleConns(5)                  // 最大空闲连接数
	conn.SetConnMaxLifetime(5 * time.Minute) // 连接最大生命周期

	if err := conn.Ping(); err != nil {
		return nil, err
	}

	db := &DB{conn: conn}
	if err := db.ensureDomainColumn(); err != nil {
		return nil, fmt.Errorf("failed to ensure domain column: %w", err)
	}

	return db, nil
}

// ensureDomainColumn adds the domain column, index, and backfills existing rows if needed.
func (db *DB) ensureDomainColumn() error {
	// Add column if not exists
	_, err := db.conn.Exec(`ALTER TABLE pages ADD COLUMN IF NOT EXISTS domain TEXT DEFAULT ''`)
	if err != nil {
		return err
	}
	// Create index if not exists
	_, err = db.conn.Exec(`CREATE INDEX IF NOT EXISTS idx_pages_domain ON pages (domain)`)
	if err != nil {
		return err
	}
	// Backfill: extract domain from url for rows where domain is empty
	_, err = db.conn.Exec(`UPDATE pages SET domain = substring(url from '://([^/]+)') WHERE domain = '' AND url != ''`)
	return err
}

// extractDomain extracts the hostname from a URL string.
func extractDomain(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Hostname()
}

func (db *DB) Close() error {
	return db.conn.Close()
}

// CreatePage 创建页面记录
func (db *DB) CreatePage(url, title, htmlPath, contentHash string, capturedAt time.Time) (int64, error) {
	var id int64
	err := db.conn.QueryRow(
		"INSERT INTO pages (url, title, html_path, content_hash, captured_at, first_visited, last_visited, domain) VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id",
		url, title, htmlPath, contentHash, capturedAt, capturedAt, capturedAt, extractDomain(url),
	).Scan(&id)
	return id, err
}

// UpdatePageBodyText 更新页面正文文本（用于全文搜索）
func (db *DB) UpdatePageBodyText(id int64, bodyText string) error {
	_, err := db.conn.Exec("UPDATE pages SET body_text = $1 WHERE id = $2", bodyText, id)
	return err
}

// GetPageByURLAndHash 根据 URL 和内容哈希查找页面
func (db *DB) GetPageByURLAndHash(url, contentHash string) (*models.Page, error) {
	var p models.Page
	err := db.conn.QueryRow(
		"SELECT id, url, title, captured_at, html_path, content_hash, first_visited, last_visited, created_at FROM pages WHERE url = $1 AND content_hash = $2",
		url, contentHash,
	).Scan(&p.ID, &p.URL, &p.Title, &p.CapturedAt, &p.HTMLPath, &p.ContentHash, &p.FirstVisited, &p.LastVisited, &p.CreatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// UpdatePageLastVisited 更新页面最后访问时间
func (db *DB) UpdatePageLastVisited(id int64, lastVisited time.Time) error {
	_, err := db.conn.Exec("UPDATE pages SET last_visited = $1 WHERE id = $2", lastVisited, id)
	return err
}

// GetResourceByHash 根据哈希查找资源
func (db *DB) GetResourceByHash(hash string) (*models.Resource, error) {
	var r models.Resource
	err := db.conn.QueryRow(
		"SELECT id, url, content_hash, resource_type, file_path, file_size, first_seen, last_seen FROM resources WHERE content_hash = $1",
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
func (db *DB) CreateResource(url, hash, resourceType, filePath string, fileSize int64) (int64, error) {
	var id int64
	err := db.conn.QueryRow(
		"INSERT INTO resources (url, content_hash, resource_type, file_path, file_size) VALUES ($1, $2, $3, $4, $5) RETURNING id",
		url, hash, resourceType, filePath, fileSize,
	).Scan(&id)
	return id, err
}

// CreateResourceIfNotExists 创建资源记录，如果 URL 已存在则返回现有记录（防止竞态）
func (db *DB) CreateResourceIfNotExists(url, hash, resourceType, filePath string, fileSize int64) (int64, error) {
	var id int64
	err := db.conn.QueryRow(`
		INSERT INTO resources (url, content_hash, resource_type, file_path, file_size)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (url) DO UPDATE SET last_seen = NOW()
		RETURNING id
	`, url, hash, resourceType, filePath, fileSize).Scan(&id)
	return id, err
}

// UpdateResourceLastSeen 更新资源最后见到时间
func (db *DB) UpdateResourceLastSeen(id int64) error {
	_, err := db.conn.Exec("UPDATE resources SET last_seen = NOW() WHERE id = $1", id)
	return err
}

// LinkPageResource 关联页面和资源
func (db *DB) LinkPageResource(pageID, resourceID int64) error {
	_, err := db.conn.Exec(
		"INSERT INTO page_resources (page_id, resource_id) VALUES ($1, $2) ON CONFLICT DO NOTHING",
		pageID, resourceID,
	)
	return err
}

// CheckRecentCapture 检查最近是否已捕获相同 URL（5分钟内）
func (db *DB) CheckRecentCapture(url string, within time.Duration) (bool, error) {
	var count int
	err := db.conn.QueryRow(
		"SELECT COUNT(*) FROM pages WHERE url = $1 AND captured_at > $2",
		url, time.Now().Add(-within),
	).Scan(&count)
	return count > 0, err
}

// GetResourceByID 根据 ID 获取资源
func (db *DB) GetResourceByID(id int64) (*models.Resource, error) {
	var r models.Resource
	err := db.conn.QueryRow(
		"SELECT id, url, content_hash, resource_type, file_path, file_size, first_seen, last_seen FROM resources WHERE id = $1",
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
func (db *DB) GetResourceByURL(url string) (*models.Resource, error) {
	var r models.Resource
	err := db.conn.QueryRow(
		"SELECT id, url, content_hash, resource_type, file_path, file_size, first_seen, last_seen FROM resources WHERE url = $1 ORDER BY last_seen DESC LIMIT 1",
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
func (db *DB) GetResourceByURLLike(pattern string) (*models.Resource, error) {
	var r models.Resource
	err := db.conn.QueryRow(
		"SELECT id, url, content_hash, resource_type, file_path, file_size, first_seen, last_seen FROM resources WHERE url LIKE $1 ORDER BY last_seen DESC LIMIT 1",
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
func (db *DB) ListPages(limit, offset int, from, to *time.Time, domain string) ([]models.Page, error) {
	query := "SELECT id, url, title, captured_at, html_path, content_hash, first_visited, last_visited, created_at FROM pages"
	args := []interface{}{}
	argIndex := 1

	// 构建 WHERE 条件
	var conditions []string
	if from != nil {
		conditions = append(conditions, fmt.Sprintf("captured_at >= $%d", argIndex))
		args = append(args, *from)
		argIndex++
	}
	if to != nil {
		// to 使用 < nextDay 确保包含当天全部记录
		nextDay := to.AddDate(0, 0, 1)
		conditions = append(conditions, fmt.Sprintf("captured_at < $%d", argIndex))
		args = append(args, nextDay)
		argIndex++
	}
	if domain != "" {
		conditions = append(conditions, fmt.Sprintf("(domain = $%d OR domain LIKE $%d)", argIndex, argIndex+1))
		args = append(args, domain, "%."+domain)
		argIndex += 2
	}

	if len(conditions) > 0 {
		query += " WHERE " + conditions[0]
		for i := 1; i < len(conditions); i++ {
			query += " AND " + conditions[i]
		}
	}

	query += fmt.Sprintf(" ORDER BY last_visited DESC LIMIT $%d OFFSET $%d", argIndex, argIndex+1)
	args = append(args, limit, offset)

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pages []models.Page
	for rows.Next() {
		var p models.Page
		if err := rows.Scan(&p.ID, &p.URL, &p.Title, &p.CapturedAt, &p.HTMLPath, &p.ContentHash, &p.FirstVisited, &p.LastVisited, &p.CreatedAt); err != nil {
			return nil, err
		}
		pages = append(pages, p)
	}
	return pages, nil
}

// GetTotalPagesCount 获取页面总数（支持时间和域名过滤）
func (db *DB) GetTotalPagesCount(from, to *time.Time, domain string) (int, error) {
	query := "SELECT COUNT(*) FROM pages"
	args := []interface{}{}
	argIndex := 1

	// 构建 WHERE 条件
	var conditions []string
	if from != nil {
		conditions = append(conditions, fmt.Sprintf("captured_at >= $%d", argIndex))
		args = append(args, *from)
		argIndex++
	}
	if to != nil {
		// to 使用 < nextDay 确保包含当天全部记录
		nextDay := to.AddDate(0, 0, 1)
		conditions = append(conditions, fmt.Sprintf("captured_at < $%d", argIndex))
		args = append(args, nextDay)
		argIndex++
	}
	if domain != "" {
		conditions = append(conditions, fmt.Sprintf("(domain = $%d OR domain LIKE $%d)", argIndex, argIndex+1))
		args = append(args, domain, "%."+domain)
		argIndex += 2
	}

	if len(conditions) > 0 {
		query += " WHERE " + conditions[0]
		for i := 1; i < len(conditions); i++ {
			query += " AND " + conditions[i]
		}
	}

	var count int
	err := db.conn.QueryRow(query, args...).Scan(&count)
	return count, err
}

// GetPageByID 根据 ID 获取页面
func (db *DB) GetPageByID(id string) (*models.Page, error) {
	var p models.Page
	err := db.conn.QueryRow(
		"SELECT id, url, title, captured_at, html_path, content_hash, first_visited, last_visited, created_at FROM pages WHERE id = $1",
		id,
	).Scan(&p.ID, &p.URL, &p.Title, &p.CapturedAt, &p.HTMLPath, &p.ContentHash, &p.FirstVisited, &p.LastVisited, &p.CreatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// SearchPages 搜索页面（按 URL、标题或正文内容，支持时间和域名过滤）
func (db *DB) SearchPages(keyword string, from, to *time.Time, domain string) ([]models.Page, error) {
	query := "SELECT id, url, title, captured_at, html_path, content_hash, first_visited, last_visited, created_at FROM pages WHERE (url ILIKE $1 OR title ILIKE $1 OR body_text ILIKE $1)"
	args := []interface{}{"%"+keyword+"%"}
	argIndex := 2

	// 追加时间过滤条件
	if from != nil {
		query += fmt.Sprintf(" AND captured_at >= $%d", argIndex)
		args = append(args, *from)
		argIndex++
	}
	if to != nil {
		// to 使用 < nextDay 确保包含当天全部记录
		nextDay := to.AddDate(0, 0, 1)
		query += fmt.Sprintf(" AND captured_at < $%d", argIndex)
		args = append(args, nextDay)
		argIndex++
	}
	if domain != "" {
		query += fmt.Sprintf(" AND (domain = $%d OR domain LIKE $%d)", argIndex, argIndex+1)
		args = append(args, domain, "%."+domain)
		argIndex += 2
	}

	query += " ORDER BY last_visited DESC LIMIT 100"

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pages []models.Page
	for rows.Next() {
		var p models.Page
		if err := rows.Scan(&p.ID, &p.URL, &p.Title, &p.CapturedAt, &p.HTMLPath, &p.ContentHash, &p.FirstVisited, &p.LastVisited, &p.CreatedAt); err != nil {
			return nil, err
		}
		pages = append(pages, p)
	}
	return pages, nil
}

// GetPagesWithoutBodyText 获取所有没有 body_text 的页面（用于回填）
func (db *DB) GetPagesWithoutBodyText() ([]models.Page, error) {
	rows, err := db.conn.Query(
		"SELECT id, url, title, captured_at, html_path, content_hash, first_visited, last_visited, created_at FROM pages WHERE body_text IS NULL ORDER BY id",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pages []models.Page
	for rows.Next() {
		var p models.Page
		if err := rows.Scan(&p.ID, &p.URL, &p.Title, &p.CapturedAt, &p.HTMLPath, &p.ContentHash, &p.FirstVisited, &p.LastVisited, &p.CreatedAt); err != nil {
			return nil, err
		}
		pages = append(pages, p)
	}
	return pages, nil
}

// GetResourcesByPageID 获取页面关联的所有资源
func (db *DB) GetResourcesByPageID(pageID int64) ([]models.Resource, error) {
	rows, err := db.conn.Query(`
		SELECT r.id, r.url, r.content_hash, r.resource_type, r.file_path, r.file_size, r.first_seen, r.last_seen
		FROM resources r
		INNER JOIN page_resources pr ON r.id = pr.resource_id
		WHERE pr.page_id = $1
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
func (db *DB) GetResourceByURLAndPageID(url string, pageID int64) (*models.Resource, error) {
	var r models.Resource
	err := db.conn.QueryRow(`
		SELECT r.id, r.url, r.content_hash, r.resource_type, r.file_path, r.file_size, r.first_seen, r.last_seen
		FROM resources r
		INNER JOIN page_resources pr ON r.id = pr.resource_id
		WHERE pr.page_id = $1 AND r.url = $2
		LIMIT 1
	`, pageID, url).Scan(&r.ID, &r.URL, &r.ContentHash, &r.ResourceType, &r.FilePath, &r.FileSize, &r.FirstSeen, &r.LastSeen)

	if err == sql.ErrNoRows {
		// 如果页面关联中没有，尝试直接按URL查找最新的
		return db.GetResourceByURL(url)
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// GetResourceByURLPrefix 根据 URL 前缀匹配资源（处理 DB 中 URL 带 #fragment 的情况）
func (db *DB) GetResourceByURLPrefix(urlPrefix string, pageID int64) (*models.Resource, error) {
	var r models.Resource
	err := db.conn.QueryRow(`
		SELECT r.id, r.url, r.content_hash, r.resource_type, r.file_path, r.file_size, r.first_seen, r.last_seen
		FROM resources r
		INNER JOIN page_resources pr ON r.id = pr.resource_id
		WHERE pr.page_id = $1 AND (r.url LIKE $2 OR r.url LIKE $3)
		LIMIT 1
	`, pageID, urlPrefix+"#%", urlPrefix+"%23%").Scan(&r.ID, &r.URL, &r.ContentHash, &r.ResourceType, &r.FilePath, &r.FileSize, &r.FirstSeen, &r.LastSeen)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// GetResourceByURLPath 根据 URL 路径匹配资源（忽略查询参数，用于同一图片不同 token 的情况）
func (db *DB) GetResourceByURLPath(urlPath string, pageID int64) (*models.Resource, error) {
	var r models.Resource
	err := db.conn.QueryRow(`
		SELECT r.id, r.url, r.content_hash, r.resource_type, r.file_path, r.file_size, r.first_seen, r.last_seen
		FROM resources r
		INNER JOIN page_resources pr ON r.id = pr.resource_id
		WHERE pr.page_id = $1 AND (r.url = $2 OR r.url LIKE $3)
		LIMIT 1
	`, pageID, urlPath, urlPath+"?%").Scan(&r.ID, &r.URL, &r.ContentHash, &r.ResourceType, &r.FilePath, &r.FileSize, &r.FirstSeen, &r.LastSeen)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// UpdatePageContent 更新页面内容（HTML路径、哈希、标题、最后访问时间）
func (db *DB) UpdatePageContent(id int64, htmlPath, contentHash, title string) error {
	_, err := db.conn.Exec(
		"UPDATE pages SET html_path = $1, content_hash = $2, title = $3, last_visited = NOW() WHERE id = $4",
		htmlPath, contentHash, title, id,
	)
	return err
}

// GetPagesByURL 获取同一 URL 的所有快照（按时间倒序）
func (db *DB) GetPagesByURL(pageURL string) ([]models.Page, error) {
	rows, err := db.conn.Query(
		"SELECT id, url, title, captured_at, html_path, content_hash, first_visited, last_visited, created_at FROM pages WHERE url = $1 ORDER BY first_visited DESC",
		pageURL,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pages []models.Page
	for rows.Next() {
		var p models.Page
		if err := rows.Scan(&p.ID, &p.URL, &p.Title, &p.CapturedAt, &p.HTMLPath, &p.ContentHash, &p.FirstVisited, &p.LastVisited, &p.CreatedAt); err != nil {
			return nil, err
		}
		pages = append(pages, p)
	}
	return pages, nil
}

// GetSnapshotNeighbors 获取某个快照的前后快照（用于导航）
func (db *DB) GetSnapshotNeighbors(pageURL string, currentID int64) (prev *models.Page, next *models.Page, total int, err error) {
	// 获取总数
	err = db.conn.QueryRow("SELECT COUNT(*) FROM pages WHERE url = $1", pageURL).Scan(&total)
	if err != nil {
		return
	}

	// 前一个快照（比当前更早）
	var p models.Page
	err = db.conn.QueryRow(
		"SELECT id, url, title, captured_at, html_path, content_hash, first_visited, last_visited, created_at FROM pages WHERE url = $1 AND first_visited < (SELECT first_visited FROM pages WHERE id = $2) ORDER BY first_visited DESC LIMIT 1",
		pageURL, currentID,
	).Scan(&p.ID, &p.URL, &p.Title, &p.CapturedAt, &p.HTMLPath, &p.ContentHash, &p.FirstVisited, &p.LastVisited, &p.CreatedAt)
	if err == nil {
		prev = &p
	} else if err != sql.ErrNoRows {
		return
	}
	err = nil

	// 后一个快照（比当前更新）
	var n models.Page
	err = db.conn.QueryRow(
		"SELECT id, url, title, captured_at, html_path, content_hash, first_visited, last_visited, created_at FROM pages WHERE url = $1 AND first_visited > (SELECT first_visited FROM pages WHERE id = $2) ORDER BY first_visited ASC LIMIT 1",
		pageURL, currentID,
	).Scan(&n.ID, &n.URL, &n.Title, &n.CapturedAt, &n.HTMLPath, &n.ContentHash, &n.FirstVisited, &n.LastVisited, &n.CreatedAt)
	if err == nil {
		next = &n
	} else if err != sql.ErrNoRows {
		return
	}
	err = nil
	return
}

// DeletePageResources 删除页面资源关联（不删除资源本身）
func (db *DB) DeletePageResources(pageID int64) error {
	_, err := db.conn.Exec("DELETE FROM page_resources WHERE page_id = $1", pageID)
	return err
}

// DeletePage 删除页面记录（包括关联关系）
func (db *DB) DeletePage(id int64) error {
	// 开启事务
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 删除页面资源关联
	_, err = tx.Exec("DELETE FROM page_resources WHERE page_id = $1", id)
	if err != nil {
		return err
	}

	// 删除页面记录
	_, err = tx.Exec("DELETE FROM pages WHERE id = $1", id)
	if err != nil {
		return err
	}

	return tx.Commit()
}
