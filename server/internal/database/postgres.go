package database

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
	"wayback/internal/models"
)

//go:embed schema/init_postgres.sql
var postgresSchema string

// PostgresDB PostgreSQL 数据库实现
type PostgresDB struct {
	conn                   *sql.DB
	qb                     *QueryBuilder
	searchTextTrigramReady bool
}

const postgresSearchTextExpression = "(COALESCE(url, '') || E'\\n' || COALESCE(title, '') || E'\\n' || COALESCE(body_text, ''))"

// 保持向后兼容的类型别名
type DB = PostgresDB

// New 创建 PostgreSQL 数据库连接（向后兼容函数）
// 推荐使用 NewPostgres 或 Open 函数
func New(host, port, user, password, dbname string, sslmode ...string) (Database, error) {
	return NewPostgres(host, port, user, password, dbname, sslmode...)
}

// NewPostgres 创建 PostgreSQL 数据库连接
func NewPostgres(host, port, user, password, dbname string, sslmode ...string) (Database, error) {
	mode := "disable"
	if len(sslmode) > 0 && sslmode[0] != "" {
		mode = sslmode[0]
	}

	conn, err := openPostgresConnection(host, port, user, password, dbname, mode)
	if err != nil {
		if !isMissingDatabaseError(err) {
			return nil, err
		}

		if err := ensurePostgresDatabase(host, port, user, password, dbname, mode); err != nil {
			return nil, err
		}

		conn, err = openPostgresConnection(host, port, user, password, dbname, mode)
		if err != nil {
			return nil, err
		}
	}

	db := &PostgresDB{
		conn: conn,
		qb:   NewQueryBuilder(DBTypePostgreSQL),
	}
	if err := db.ensurePostgresMigrations(); err != nil {
		conn.Close()
		return nil, err
	}
	return db, nil
}

func (db *PostgresDB) ensurePostgresMigrations() error {
	return db.withPostgresMigrationLock(func() error {
		if err := db.ensureSchema(); err != nil {
			return fmt.Errorf("failed to initialize PostgreSQL schema: %w", err)
		}
		if err := db.ensureResourcesContentHashNotUnique(); err != nil {
			return fmt.Errorf("failed to ensure resources content_hash is not unique: %w", err)
		}
		if err := db.ensureDomainColumn(); err != nil {
			return fmt.Errorf("failed to ensure domain column: %w", err)
		}
		if err := db.ensureSnapshotStateColumn(); err != nil {
			return fmt.Errorf("failed to ensure snapshot_state column: %w", err)
		}
		if err := db.ensureResourceQuarantineColumns(); err != nil {
			return fmt.Errorf("failed to ensure resource quarantine columns: %w", err)
		}
		if err := db.ensurePageShareTables(); err != nil {
			return fmt.Errorf("failed to ensure page share tables: %w", err)
		}
		searchTextTrigramReady, err := db.ensureSearchIndexes()
		if err != nil {
			return fmt.Errorf("failed to ensure search indexes: %w", err)
		}
		db.searchTextTrigramReady = searchTextTrigramReady
		return nil
	})
}

func (db *PostgresDB) withPostgresMigrationLock(fn func() error) error {
	ctx := context.Background()
	conn, err := db.conn.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock(20260530, 447392260)`); err != nil {
		return err
	}
	defer func() {
		_, _ = conn.ExecContext(ctx, `SELECT pg_advisory_unlock(20260530, 447392260)`)
	}()

	return fn()
}

func openPostgresConnection(host, port, user, password, dbname, sslmode string) (*sql.DB, error) {
	connStr := buildConnectionString(host, port, user, password, dbname, sslmode)

	conn, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, err
	}

	// 配置连接池
	conn.SetMaxOpenConns(25)                 // 最大打开连接数
	conn.SetMaxIdleConns(5)                  // 最大空闲连接数
	conn.SetConnMaxLifetime(5 * time.Minute) // 连接最大生命周期

	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, err
	}

	return conn, nil
}

func buildConnectionString(host, port, user, password, dbname, sslmode string) string {
	parts := []string{
		fmt.Sprintf("host=%s", quoteConnValue(host)),
		fmt.Sprintf("port=%s", quoteConnValue(port)),
		fmt.Sprintf("dbname=%s", quoteConnValue(dbname)),
		fmt.Sprintf("sslmode=%s", quoteConnValue(sslmode)),
	}

	if user != "" {
		parts = append(parts, fmt.Sprintf("user=%s", quoteConnValue(user)))
	}
	if password != "" {
		parts = append(parts, fmt.Sprintf("password=%s", quoteConnValue(password)))
	}

	return strings.Join(parts, " ")
}

func quoteConnValue(value string) string {
	escaped := strings.ReplaceAll(value, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `'`, `\'`)
	return "'" + escaped + "'"
}

func ensurePostgresDatabase(host, port, user, password, dbname, sslmode string) error {
	for _, maintenanceDB := range maintenanceDatabaseNames(dbname) {
		conn, err := openPostgresConnection(host, port, user, password, maintenanceDB, sslmode)
		if err != nil {
			if isMissingDatabaseError(err) {
				continue
			}
			return fmt.Errorf("failed to connect to PostgreSQL maintenance database %q: %w", maintenanceDB, err)
		}

		err = createDatabaseIfMissing(conn, dbname)
		conn.Close()
		if err != nil {
			return err
		}
		return nil
	}

	return fmt.Errorf("failed to create PostgreSQL database %q: no available maintenance database", dbname)
}

func maintenanceDatabaseNames(targetDB string) []string {
	names := []string{"postgres", "template1"}
	if targetDB == "" {
		return names
	}

	filtered := make([]string, 0, len(names))
	for _, name := range names {
		if name != targetDB {
			filtered = append(filtered, name)
		}
	}
	if len(filtered) == 0 {
		return names
	}
	return filtered
}

func createDatabaseIfMissing(conn *sql.DB, dbname string) error {
	var exists int
	err := conn.QueryRow("SELECT 1 FROM pg_database WHERE datname = $1", dbname).Scan(&exists)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("failed to check PostgreSQL database %q: %w", dbname, err)
	}

	_, err = conn.Exec("CREATE DATABASE " + pq.QuoteIdentifier(dbname))
	if err != nil && !isDuplicateDatabaseError(err) {
		return fmt.Errorf("failed to create PostgreSQL database %q: %w", dbname, err)
	}

	return nil
}

func isMissingDatabaseError(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "3D000"
}

func isDuplicateDatabaseError(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "42P04"
}

func isOptionalTrigramExtensionError(err error) bool {
	var pqErr *pq.Error
	if !errors.As(err, &pqErr) {
		return false
	}

	switch pqErr.Code {
	case "42501", "58P01":
		return true
	default:
		return false
	}
}

func (db *PostgresDB) ensureSchema() error {
	if err := db.ensureTrigramExtension(); err != nil {
		return err
	}
	if _, err := db.conn.Exec(postgresSchema); err != nil {
		return err
	}
	return nil
}

func (db *PostgresDB) ensureTrigramExtension() error {
	_, err := db.conn.Exec(`CREATE EXTENSION IF NOT EXISTS pg_trgm`)
	if err == nil || isOptionalTrigramExtensionError(err) {
		return nil
	}
	return err
}

func (db *PostgresDB) ensureSearchIndexes() (bool, error) {
	if _, err := db.conn.Exec(`CREATE INDEX IF NOT EXISTS idx_pages_activity_desc ON pages ((COALESCE(last_visited, captured_at)) DESC, id DESC)`); err != nil {
		return false, err
	}

	hasTrigram, err := db.hasPostgresExtension("pg_trgm")
	if err != nil || !hasTrigram {
		return false, err
	}

	if _, err := db.conn.Exec(`CREATE INDEX IF NOT EXISTS idx_pages_search_text_trgm ON pages USING gin (` + postgresSearchTextExpression + ` gin_trgm_ops)`); err != nil {
		return false, err
	}
	// The combined expression index replaces the old per-column trigram indexes.
	// Cleanup is best-effort so a permission issue cannot prevent startup after
	// the new search path is ready.
	_ = db.dropLegacySearchTrigramIndexes()
	return true, nil
}

func (db *PostgresDB) hasPostgresExtension(name string) (bool, error) {
	var exists bool
	err := db.conn.QueryRow(`SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = $1)`, name).Scan(&exists)
	return exists, err
}

func (db *PostgresDB) dropLegacySearchTrigramIndexes() error {
	_, err := db.conn.Exec(`
DROP INDEX IF EXISTS idx_pages_body_text_trgm;
DROP INDEX IF EXISTS idx_pages_title_trgm;
`)
	return err
}

func (db *PostgresDB) ensureResourcesContentHashNotUnique() error {
	_, err := db.conn.Exec(`ALTER TABLE resources DROP CONSTRAINT IF EXISTS resources_content_hash_key`)
	return err
}

// ensureDomainColumn adds the domain column, index, and backfills existing rows if needed.
func (db *PostgresDB) ensureDomainColumn() error {
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
	domainSQL := db.qb.ExtractDomain("url")
	query := fmt.Sprintf("UPDATE pages SET domain = %s WHERE domain = '' AND url != ''", domainSQL)
	_, err = db.conn.Exec(query)
	return err
}

func (db *PostgresDB) ensureSnapshotStateColumn() error {
	if _, err := db.conn.Exec(`ALTER TABLE pages ADD COLUMN IF NOT EXISTS snapshot_state VARCHAR(16)`); err != nil {
		return err
	}
	if _, err := db.conn.Exec(`UPDATE pages SET snapshot_state = 'ready' WHERE snapshot_state IS NULL OR snapshot_state = ''`); err != nil {
		return err
	}
	if _, err := db.conn.Exec(`ALTER TABLE pages ALTER COLUMN snapshot_state SET DEFAULT 'pending'`); err != nil {
		return err
	}
	if _, err := db.conn.Exec(`ALTER TABLE pages ALTER COLUMN snapshot_state SET NOT NULL`); err != nil {
		return err
	}
	return nil
}

func (db *PostgresDB) ensureResourceQuarantineColumns() error {
	if _, err := db.conn.Exec(`ALTER TABLE resources ADD COLUMN IF NOT EXISTS is_quarantined BOOLEAN NOT NULL DEFAULT FALSE`); err != nil {
		return err
	}
	if _, err := db.conn.Exec(`ALTER TABLE resources ADD COLUMN IF NOT EXISTS quarantine_reason TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if _, err := db.conn.Exec(`UPDATE resources SET quarantine_reason = '' WHERE quarantine_reason IS NULL`); err != nil {
		return err
	}
	return nil
}

func (db *PostgresDB) ensurePageShareTables() error {
	_, err := db.conn.Exec(`
CREATE TABLE IF NOT EXISTS page_shares (
    id BIGSERIAL PRIMARY KEY,
    token_hash TEXT NOT NULL UNIQUE,
    page_id BIGINT REFERENCES pages(id) ON DELETE CASCADE,
    url TEXT NOT NULL,
    title TEXT,
    html_path TEXT NOT NULL,
    content_hash CHAR(64),
    captured_at TIMESTAMP WITH TIME ZONE NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMP WITH TIME ZONE,
    revoked_at TIMESTAMP WITH TIME ZONE,
    allow_markdown BOOLEAN NOT NULL DEFAULT TRUE
);
CREATE INDEX IF NOT EXISTS idx_page_shares_page ON page_shares(page_id);
CREATE INDEX IF NOT EXISTS idx_page_shares_html_path ON page_shares(html_path);
CREATE TABLE IF NOT EXISTS page_share_resources (
    token_hash TEXT NOT NULL REFERENCES page_shares(token_hash) ON DELETE CASCADE,
    resource_id BIGINT NOT NULL REFERENCES resources(id) ON DELETE CASCADE,
    PRIMARY KEY (token_hash, resource_id)
);
CREATE INDEX IF NOT EXISTS idx_page_share_resources_resource ON page_share_resources(resource_id);
`)
	return err
}

func (db *PostgresDB) Close() error {
	return db.conn.Close()
}

// CreatePage 创建页面记录
func (db *PostgresDB) CreatePage(url, title, htmlPath, contentHash string, capturedAt time.Time) (int64, error) {
	var id int64
	err := db.conn.QueryRow(
		"INSERT INTO pages (url, title, html_path, content_hash, snapshot_state, captured_at, first_visited, last_visited, domain) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id",
		url, title, htmlPath, contentHash, models.SnapshotStatePending, capturedAt.UTC(), capturedAt.UTC(), capturedAt.UTC(), extractDomain(url),
	).Scan(&id)
	return id, err
}

// UpdatePageBodyText 更新页面正文文本（用于全文搜索）
func (db *PostgresDB) UpdatePageBodyText(id int64, bodyText string) error {
	_, err := db.conn.Exec("UPDATE pages SET body_text = $1 WHERE id = $2", bodyText, id)
	return err
}

// GetPageByURLAndHash 根据 URL 和内容哈希查找页面
func (db *PostgresDB) GetPageByURLAndHash(url, contentHash string) (*models.Page, error) {
	var p models.Page
	row := db.conn.QueryRow(
		"SELECT "+pageSelectColumns+" FROM pages WHERE url = $1 AND content_hash = $2 ORDER BY CASE snapshot_state WHEN 'ready' THEN 0 WHEN 'pending' THEN 1 ELSE 2 END, first_visited ASC LIMIT 1",
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
func (db *PostgresDB) UpdatePageLastVisited(id int64, lastVisited time.Time) error {
	_, err := db.conn.Exec("UPDATE pages SET last_visited = $1 WHERE id = $2", lastVisited.UTC(), id)
	return err
}

// GetResourceByHash 根据哈希查找资源
func (db *PostgresDB) GetResourceByHash(hash string) (*models.Resource, error) {
	var r models.Resource
	err := scanResource(db.conn.QueryRow(
		"SELECT "+resourceSelectColumns+" FROM resources WHERE content_hash = $1 AND is_quarantined = FALSE ORDER BY last_seen DESC LIMIT 1",
		hash,
	), &r)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// CreateResource 创建资源记录
func (db *PostgresDB) CreateResource(url, hash, resourceType, filePath string, fileSize int64) (int64, error) {
	var id int64
	now := time.Now().UTC()
	err := db.conn.QueryRow(
		"INSERT INTO resources (url, content_hash, resource_type, file_path, file_size, first_seen, last_seen) VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id",
		url, hash, resourceType, filePath, fileSize, now, now,
	).Scan(&id)
	return id, err
}

// UpdateResourceLastSeen 更新资源最后见到时间
func (db *PostgresDB) UpdateResourceLastSeen(id int64) error {
	now := time.Now().UTC()
	_, err := db.conn.Exec("UPDATE resources SET last_seen = $1 WHERE id = $2", now, id)
	return err
}

func (db *PostgresDB) touchResourcesLastSeen(tx *sql.Tx, resourceIDs []int64) error {
	if len(resourceIDs) == 0 {
		return nil
	}
	now := time.Now().UTC()
	_, err := tx.Exec("UPDATE resources SET last_seen = $1 WHERE id = ANY($2)", now, pq.Array(resourceIDs))
	return err
}

// LinkPageResource 关联页面和资源
func (db *PostgresDB) LinkPageResource(pageID, resourceID int64) error {
	_, err := db.conn.Exec(
		"INSERT INTO page_resources (page_id, resource_id) VALUES ($1, $2) ON CONFLICT DO NOTHING",
		pageID, resourceID,
	)
	return err
}

// LinkPageResources links a page to all provided resources in a single transaction.
func (db *PostgresDB) LinkPageResources(pageID int64, resourceIDs []int64) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, resourceID := range resourceIDs {
		if _, err := tx.Exec(
			"INSERT INTO page_resources (page_id, resource_id) VALUES ($1, $2) ON CONFLICT DO NOTHING",
			pageID, resourceID,
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// CheckRecentCapture 检查最近是否已捕获相同 URL（5分钟内）
func (db *PostgresDB) CheckRecentCapture(url string, within time.Duration) (bool, error) {
	var count int
	err := db.conn.QueryRow(
		"SELECT COUNT(*) FROM pages WHERE url = $1 AND captured_at > $2",
		url, time.Now().Add(-within),
	).Scan(&count)
	return count > 0, err
}

// GetResourceByID 根据 ID 获取资源
func (db *PostgresDB) GetResourceByID(id int64) (*models.Resource, error) {
	var r models.Resource
	err := scanResource(db.conn.QueryRow("SELECT "+resourceSelectColumns+" FROM resources WHERE id = $1", id), &r)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// GetResourceByURL 根据 URL 获取资源（返回最新的）
func (db *PostgresDB) GetResourceByURL(url string) (*models.Resource, error) {
	return db.getAnyResourceByURL(url, false)
}

func (db *PostgresDB) getAnyResourceByURL(url string, includeQuarantined bool) (*models.Resource, error) {
	var r models.Resource
	query := "SELECT " + resourceSelectColumns + " FROM resources WHERE url = $1"
	if !includeQuarantined {
		query += " AND is_quarantined = FALSE"
	}
	query += " ORDER BY last_seen DESC LIMIT 1"
	err := scanResource(db.conn.QueryRow(query, url), &r)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// GetResourceByURLLike 根据 URL 模糊匹配查找资源（如忽略查询参数差异）
func (db *PostgresDB) GetResourceByURLLike(pattern string) (*models.Resource, error) {
	var r models.Resource
	err := scanResource(db.conn.QueryRow(
		"SELECT "+resourceSelectColumns+" FROM resources WHERE url LIKE $1 AND is_quarantined = FALSE ORDER BY last_seen DESC LIMIT 1",
		pattern,
	), &r)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// ListPages 列出页面（分页，支持时间和域名过滤）
func (db *PostgresDB) ListPages(limit, offset int, from, to *time.Time, domain string) ([]models.Page, error) {
	query := "SELECT " + pageSelectColumns + " FROM pages"
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
func (db *PostgresDB) GetTotalPagesCount(from, to *time.Time, domain string) (int, error) {
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
func (db *PostgresDB) GetPageByID(id string) (*models.Page, error) {
	var p models.Page
	row := db.conn.QueryRow(
		"SELECT "+pageSelectColumns+" FROM pages WHERE id = $1",
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

func (db *PostgresDB) buildSearchWhere(keyword string, from, to *time.Time, domain string) (string, []interface{}, int) {
	likeOp := db.qb.CaseInsensitiveLike()
	where := fmt.Sprintf(" WHERE %s %s $1 ESCAPE '\\'", postgresSearchTextExpression, likeOp)
	args := []interface{}{"%" + escapeLikePattern(keyword) + "%"}
	argIndex := 2

	// 追加时间过滤条件
	if from != nil {
		where += fmt.Sprintf(" AND captured_at >= $%d", argIndex)
		args = append(args, *from)
		argIndex++
	}
	if to != nil {
		// to 使用 < nextDay 确保包含当天全部记录
		nextDay := to.AddDate(0, 0, 1)
		where += fmt.Sprintf(" AND captured_at < $%d", argIndex)
		args = append(args, nextDay)
		argIndex++
	}
	if domain != "" {
		where += fmt.Sprintf(" AND (domain = $%d OR domain LIKE $%d)", argIndex, argIndex+1)
		args = append(args, domain, "%."+domain)
		argIndex += 2
	}

	return where, args, argIndex
}

// SearchPages 搜索页面（按 URL、标题或正文内容，支持分页、时间和域名过滤）
func (db *PostgresDB) SearchPages(keyword string, limit, offset int, from, to *time.Time, domain string) ([]models.Page, error) {
	where, args, argIndex := db.buildSearchWhere(keyword, from, to, domain)
	query := fmt.Sprintf(
		"SELECT %s FROM pages%s ORDER BY COALESCE(last_visited, captured_at) DESC, id DESC LIMIT $%d OFFSET $%d",
		pageSearchSelectColumns,
		where,
		argIndex,
		argIndex+1,
	)
	args = append(args, limit, offset)

	if db.shouldForceSearchTextTrigram(keyword) {
		return db.searchPagesWithTrigramPlan(query, args, keyword)
	}

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanPostgresSearchPages(rows, keyword)
}

func (db *PostgresDB) shouldForceSearchTextTrigram(keyword string) bool {
	return db.searchTextTrigramReady && len([]rune(keyword)) >= 3
}

func (db *PostgresDB) searchPagesWithTrigramPlan(query string, args []interface{}, keyword string) ([]models.Page, error) {
	tx, err := db.conn.Begin()
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if _, err := tx.Exec(`SET LOCAL enable_seqscan = off`); err != nil {
		return nil, err
	}

	rows, err := tx.Query(query, args...)
	if err != nil {
		return nil, err
	}

	pages, err := scanPostgresSearchPages(rows, keyword)
	closeErr := rows.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true
	return pages, nil
}

func scanPostgresSearchPages(rows *sql.Rows, keyword string) ([]models.Page, error) {
	pages := []models.Page{}
	for rows.Next() {
		var p models.Page
		var bodyText string
		if err := scanSearchPage(rows, &p, &bodyText); err != nil {
			return nil, err
		}
		applySearchHighlights(&p, keyword, bodyText)
		pages = append(pages, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return pages, nil
}

// GetSearchPagesCount 获取搜索结果总数（支持时间和域名过滤）
func (db *PostgresDB) GetSearchPagesCount(keyword string, from, to *time.Time, domain string) (int, error) {
	where, args, _ := db.buildSearchWhere(keyword, from, to, domain)
	query := "SELECT COUNT(*) FROM pages" + where

	if db.shouldForceSearchTextTrigram(keyword) {
		return db.getSearchPagesCountWithTrigramPlan(query, args)
	}

	var count int
	err := db.conn.QueryRow(query, args...).Scan(&count)
	return count, err
}

func (db *PostgresDB) getSearchPagesCountWithTrigramPlan(query string, args []interface{}) (int, error) {
	tx, err := db.conn.Begin()
	if err != nil {
		return 0, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if _, err := tx.Exec(`SET LOCAL enable_seqscan = off`); err != nil {
		return 0, err
	}

	var count int
	if err := tx.QueryRow(query, args...).Scan(&count); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	committed = true
	return count, nil
}

// GetPagesWithoutBodyText 获取所有没有 body_text 的页面（用于回填）
func (db *PostgresDB) GetPagesWithoutBodyText() ([]models.Page, error) {
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
func (db *PostgresDB) GetResourcesByPageID(pageID int64) ([]models.Resource, error) {
	rows, err := db.conn.Query(`
		SELECT `+resourceSelectColumns+`
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
		if err := scanResource(rows, &r); err != nil {
			return nil, err
		}
		resources = append(resources, r)
	}
	return resources, nil
}

// GetResourceByURLAndPageID 根据URL和页面ID查找资源
func (db *PostgresDB) GetResourceByURLAndPageID(url string, pageID int64) (*models.Resource, error) {
	r, err := db.GetLinkedResourceByURLAndPageID(url, pageID)
	if err != nil || r != nil {
		return r, err
	}

	// 如果页面关联中没有，尝试直接按URL查找最新的
	return db.getAnyResourceByURL(url, true)
}

// GetLinkedResourceByURLAndPageID 根据URL和页面ID查找资源，只查询 page_resources 关联，不做全局兜底
func (db *PostgresDB) GetLinkedResourceByURLAndPageID(url string, pageID int64) (*models.Resource, error) {
	var r models.Resource
	err := scanResource(db.conn.QueryRow(`
		SELECT `+resourceSelectColumns+`
		FROM resources r
		INNER JOIN page_resources pr ON r.id = pr.resource_id
		WHERE pr.page_id = $1 AND r.url = $2
		LIMIT 1
	`, pageID, url), &r)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// GetResourceByURLPrefix 根据 URL 前缀匹配资源（处理 DB 中 URL 带 #fragment 的情况）
func (db *PostgresDB) GetResourceByURLPrefix(urlPrefix string, pageID int64) (*models.Resource, error) {
	var r models.Resource
	escapedPrefix := escapeLikePattern(urlPrefix)
	err := scanResource(db.conn.QueryRow(`
		SELECT `+resourceSelectColumns+`
		FROM resources r
		INNER JOIN page_resources pr ON r.id = pr.resource_id
		WHERE pr.page_id = $1 AND (r.url LIKE $2 ESCAPE '\' OR r.url LIKE $3 ESCAPE '\')
		ORDER BY r.last_seen DESC, r.id DESC
		LIMIT 1
	`, pageID, escapedPrefix+"#%", escapedPrefix+"%23%"), &r)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// GetResourceByURLPath 根据 URL 路径匹配资源（忽略查询参数，用于同一图片不同 token 的情况）
func (db *PostgresDB) GetResourceByURLPath(urlPath string, pageID int64) (*models.Resource, error) {
	var r models.Resource
	escapedPath := escapeLikePattern(urlPath)
	err := scanResource(db.conn.QueryRow(`
		SELECT `+resourceSelectColumns+`
		FROM resources r
		INNER JOIN page_resources pr ON r.id = pr.resource_id
		WHERE pr.page_id = $1 AND (r.url = $2 OR r.url LIKE $3 ESCAPE '\')
		ORDER BY CASE WHEN r.url = $2 THEN 0 ELSE 1 END, r.last_seen DESC, r.id DESC
		LIMIT 1
	`, pageID, urlPath, escapedPath+"?%"), &r)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func (db *PostgresDB) ListResourcesForIntegrityCheck(resourceType string, lastID int64, limit int) ([]models.Resource, error) {
	rows, err := db.conn.Query(
		"SELECT "+resourceSelectColumns+" FROM resources WHERE resource_type = $1 AND is_quarantined = FALSE AND id > $2 ORDER BY id ASC LIMIT $3",
		resourceType,
		lastID,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	resources := make([]models.Resource, 0, limit)
	for rows.Next() {
		var r models.Resource
		if err := scanResource(rows, &r); err != nil {
			return nil, err
		}
		resources = append(resources, r)
	}
	return resources, nil
}

func (db *PostgresDB) QuarantineResourcesByFilePath(filePath, quarantinePath, reason string) (int64, error) {
	result, err := db.conn.Exec(
		"UPDATE resources SET file_path = $1, is_quarantined = TRUE, quarantine_reason = $2 WHERE file_path = $3 AND is_quarantined = FALSE",
		quarantinePath,
		reason,
		filePath,
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// UpdatePageContent 更新页面内容（HTML路径、哈希、标题、最后访问时间）
func (db *PostgresDB) UpdatePageContent(id int64, htmlPath, contentHash, title string) error {
	now := time.Now().UTC()
	_, err := db.conn.Exec("UPDATE pages SET html_path = $1, content_hash = $2, title = $3, snapshot_state = $4, last_visited = $5 WHERE id = $6", htmlPath, contentHash, title, models.SnapshotStateReady, now, id)
	return err
}

// ReplacePageSnapshot atomically swaps the page HTML metadata and resource links.
func (db *PostgresDB) ReplacePageSnapshot(id int64, htmlPath, contentHash, title string, bodyText *string, resourceIDs []int64) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now().UTC()
	if bodyText != nil {
		if _, err := tx.Exec("UPDATE pages SET html_path = $1, content_hash = $2, title = $3, body_text = $4, snapshot_state = $5, last_visited = $6 WHERE id = $7", htmlPath, contentHash, title, *bodyText, models.SnapshotStateReady, now, id); err != nil {
			return err
		}
	} else {
		if _, err := tx.Exec("UPDATE pages SET html_path = $1, content_hash = $2, title = $3, snapshot_state = $4, last_visited = $5 WHERE id = $6", htmlPath, contentHash, title, models.SnapshotStateReady, now, id); err != nil {
			return err
		}
	}

	if _, err := tx.Exec("DELETE FROM page_resources WHERE page_id = $1", id); err != nil {
		return err
	}
	if err := db.touchResourcesLastSeen(tx, resourceIDs); err != nil {
		return err
	}

	for _, resourceID := range resourceIDs {
		if _, err := tx.Exec(
			"INSERT INTO page_resources (page_id, resource_id) VALUES ($1, $2) ON CONFLICT DO NOTHING",
			id, resourceID,
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (db *PostgresDB) ResetPageForCreateRetry(id int64, title, htmlPath string, capturedAt time.Time) (string, error) {
	tx, err := db.conn.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	var oldHTMLPath string
	if err := tx.QueryRow("SELECT html_path FROM pages WHERE id = $1 FOR UPDATE", id).Scan(&oldHTMLPath); err != nil {
		return "", err
	}
	if _, err := tx.Exec(
		"UPDATE pages SET title = $1, html_path = $2, snapshot_state = $3, last_visited = $4 WHERE id = $5",
		title, htmlPath, models.SnapshotStatePending, capturedAt.UTC(), id,
	); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return oldHTMLPath, nil
}

func (db *PostgresDB) FinalizePageCreate(id int64, resourceIDs []int64) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("UPDATE pages SET snapshot_state = $1 WHERE id = $2", models.SnapshotStateReady, id); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM page_resources WHERE page_id = $1", id); err != nil {
		return err
	}
	if err := db.touchResourcesLastSeen(tx, resourceIDs); err != nil {
		return err
	}
	for _, resourceID := range resourceIDs {
		if _, err := tx.Exec(
			"INSERT INTO page_resources (page_id, resource_id) VALUES ($1, $2) ON CONFLICT DO NOTHING",
			id, resourceID,
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (db *PostgresDB) MarkPageCreateFailed(id int64) error {
	_, err := db.conn.Exec("UPDATE pages SET snapshot_state = $1 WHERE id = $2", models.SnapshotStateFailed, id)
	return err
}

// GetPagesByURL 获取同一 URL 的所有快照（按时间倒序）
func (db *PostgresDB) GetPagesByURL(pageURL string) ([]models.Page, error) {
	rows, err := db.conn.Query(
		"SELECT "+pageSelectColumns+" FROM pages WHERE url = $1 ORDER BY first_visited DESC",
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
func (db *PostgresDB) GetSnapshotNeighbors(pageURL string, currentID int64) (prev *models.Page, next *models.Page, total int, err error) {
	// 获取总数
	err = db.conn.QueryRow("SELECT COUNT(*) FROM pages WHERE url = $1", pageURL).Scan(&total)
	if err != nil {
		return
	}

	// 前一个快照（比当前更早）
	var p models.Page
	row := db.conn.QueryRow(
		"SELECT "+pageSelectColumns+" FROM pages WHERE url = $1 AND first_visited < (SELECT first_visited FROM pages WHERE id = $2) ORDER BY first_visited DESC LIMIT 1",
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
		"SELECT "+pageSelectColumns+" FROM pages WHERE url = $1 AND first_visited > (SELECT first_visited FROM pages WHERE id = $2) ORDER BY first_visited ASC LIMIT 1",
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
func (db *PostgresDB) DeletePageResources(pageID int64) error {
	_, err := db.conn.Exec("DELETE FROM page_resources WHERE page_id = $1", pageID)
	return err
}

// DeletePage 删除页面记录（包括关联关系）
func (db *PostgresDB) DeletePage(id int64) error {
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

func (db *PostgresDB) CreatePageShare(tokenHash string, page *models.Page, resourceIDs []int64, expiresAt *time.Time, allowMarkdown bool) (*models.PageShare, error) {
	tx, err := db.conn.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	createdAt := time.Now().UTC()
	var shareID int64
	err = tx.QueryRow(
		`INSERT INTO page_shares (token_hash, page_id, url, title, html_path, content_hash, captured_at, created_at, expires_at, allow_markdown)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 RETURNING id`,
		tokenHash,
		page.ID,
		page.URL,
		page.Title,
		page.HTMLPath,
		page.ContentHash,
		page.CapturedAt.UTC(),
		createdAt,
		expiresAt,
		allowMarkdown,
	).Scan(&shareID)
	if err != nil {
		return nil, err
	}

	for _, resourceID := range resourceIDs {
		if _, err := tx.Exec(
			"INSERT INTO page_share_resources (token_hash, resource_id) VALUES ($1, $2) ON CONFLICT DO NOTHING",
			tokenHash,
			resourceID,
		); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &models.PageShare{
		ID:            shareID,
		TokenHash:     tokenHash,
		PageID:        page.ID,
		URL:           page.URL,
		Title:         page.Title,
		HTMLPath:      page.HTMLPath,
		ContentHash:   page.ContentHash,
		CapturedAt:    page.CapturedAt,
		CreatedAt:     createdAt,
		ExpiresAt:     expiresAt,
		AllowMarkdown: allowMarkdown,
	}, nil
}

func (db *PostgresDB) GetActivePageShareByTokenHash(tokenHash string) (*models.PageShare, error) {
	var share models.PageShare
	err := scanShare(db.conn.QueryRow(
		"SELECT "+shareSelectColumns+" FROM page_shares WHERE token_hash = $1 AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > $2) LIMIT 1",
		tokenHash,
		time.Now().UTC(),
	), &share)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &share, nil
}

func (db *PostgresDB) ListPageShares(pageID int64) ([]models.PageShare, error) {
	rows, err := db.conn.Query(
		"SELECT "+shareSelectColumns+" FROM page_shares WHERE page_id = $1 ORDER BY created_at DESC, id DESC",
		pageID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	shares := []models.PageShare{}
	for rows.Next() {
		var share models.PageShare
		if err := scanShare(rows, &share); err != nil {
			return nil, err
		}
		shares = append(shares, share)
	}
	return shares, rows.Err()
}

func (db *PostgresDB) RevokePageShare(id int64) error {
	_, err := db.conn.Exec("UPDATE page_shares SET revoked_at = $1 WHERE id = $2", time.Now().UTC(), id)
	return err
}

func (db *PostgresDB) GetShareResourceByURL(tokenHash, url string) (*models.Resource, error) {
	return db.getShareResourceByQuery(`
		SELECT `+resourceSelectColumns+`
		FROM resources r
		INNER JOIN page_share_resources psr ON r.id = psr.resource_id
		WHERE psr.token_hash = $1 AND r.url = $2
		LIMIT 1
	`, tokenHash, url)
}

func (db *PostgresDB) GetShareResourceByURLPrefix(tokenHash, urlPrefix string) (*models.Resource, error) {
	escapedPrefix := escapeLikePattern(urlPrefix)
	return db.getShareResourceByQuery(`
		SELECT `+resourceSelectColumns+`
		FROM resources r
		INNER JOIN page_share_resources psr ON r.id = psr.resource_id
		WHERE psr.token_hash = $1 AND (r.url LIKE $2 ESCAPE '\' OR r.url LIKE $3 ESCAPE '\')
		ORDER BY r.last_seen DESC, r.id DESC
		LIMIT 1
	`, tokenHash, escapedPrefix+"#%", escapedPrefix+"%23%")
}

func (db *PostgresDB) GetShareResourceByURLPath(tokenHash, urlPath string) (*models.Resource, error) {
	escapedPath := escapeLikePattern(urlPath)
	return db.getShareResourceByQuery(`
		SELECT `+resourceSelectColumns+`
		FROM resources r
		INNER JOIN page_share_resources psr ON r.id = psr.resource_id
		WHERE psr.token_hash = $1 AND (r.url = $2 OR r.url LIKE $3 ESCAPE '\')
		ORDER BY CASE WHEN r.url = $2 THEN 0 ELSE 1 END, r.last_seen DESC, r.id DESC
		LIMIT 1
	`, tokenHash, urlPath, escapedPath+"?%")
}

func (db *PostgresDB) GetShareResourceByFilePath(tokenHash, filePath string) (*models.Resource, error) {
	return db.getShareResourceByQuery(`
		SELECT `+resourceSelectColumns+`
		FROM resources r
		INNER JOIN page_share_resources psr ON r.id = psr.resource_id
		WHERE psr.token_hash = $1 AND r.file_path = $2
		LIMIT 1
	`, tokenHash, filePath)
}

func (db *PostgresDB) getShareResourceByQuery(query string, args ...any) (*models.Resource, error) {
	var r models.Resource
	err := scanResource(db.conn.QueryRow(query, args...), &r)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func (db *PostgresDB) HasActiveShareForHTMLPath(htmlPath string) (bool, error) {
	var count int
	err := db.conn.QueryRow(
		"SELECT COUNT(*) FROM page_shares WHERE html_path = $1 AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > $2)",
		htmlPath,
		time.Now().UTC(),
	).Scan(&count)
	return count > 0, err
}
