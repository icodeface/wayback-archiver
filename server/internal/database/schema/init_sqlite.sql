-- Wayback 数据库初始化脚本（SQLite）

-- 页面归档表
CREATE TABLE IF NOT EXISTS pages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    url TEXT NOT NULL,
    title TEXT,
    captured_at DATETIME NOT NULL,
    html_path TEXT NOT NULL,
    content_hash CHAR(64),
    snapshot_state VARCHAR(16) NOT NULL DEFAULT 'pending',
    first_visited DATETIME,
    last_visited DATETIME,
    body_text TEXT,
    domain TEXT DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_pages_url ON pages(url);
CREATE INDEX IF NOT EXISTS idx_pages_captured_at ON pages(captured_at DESC);
CREATE INDEX IF NOT EXISTS idx_pages_url_time ON pages(url, captured_at DESC);
CREATE INDEX IF NOT EXISTS idx_pages_content_hash ON pages(content_hash);
CREATE INDEX IF NOT EXISTS idx_pages_url_hash ON pages(url, content_hash);
CREATE INDEX IF NOT EXISTS idx_pages_domain ON pages(domain);

-- SQLite FTS5 全文搜索（替代 pg_trgm）
CREATE VIRTUAL TABLE IF NOT EXISTS pages_fts USING fts5(
    body_text,
    title,
    content=pages,
    content_rowid=id
);

-- 触发器：自动同步 FTS 索引
CREATE TRIGGER IF NOT EXISTS pages_fts_insert AFTER INSERT ON pages BEGIN
    INSERT INTO pages_fts(rowid, body_text, title) VALUES (new.id, new.body_text, new.title);
END;

CREATE TRIGGER IF NOT EXISTS pages_fts_update AFTER UPDATE ON pages BEGIN
    INSERT INTO pages_fts(pages_fts, rowid, body_text, title) VALUES ('delete', old.id, old.body_text, old.title);
    INSERT INTO pages_fts(rowid, body_text, title) VALUES (new.id, new.body_text, new.title);
END;

CREATE TRIGGER IF NOT EXISTS pages_fts_delete AFTER DELETE ON pages BEGIN
    INSERT INTO pages_fts(pages_fts, rowid, body_text, title) VALUES ('delete', old.id, old.body_text, old.title);
END;

-- 资源表（去重）
CREATE TABLE IF NOT EXISTS resources (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    url TEXT NOT NULL,
    content_hash CHAR(64) NOT NULL,
    resource_type VARCHAR(20),
    file_path TEXT NOT NULL,
    file_size BIGINT,
    first_seen DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_seen DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_resources_hash ON resources(content_hash);
CREATE INDEX IF NOT EXISTS idx_resources_url ON resources(url);

-- 页面-资源关联表
CREATE TABLE IF NOT EXISTS page_resources (
    page_id INTEGER REFERENCES pages(id) ON DELETE CASCADE,
    resource_id INTEGER REFERENCES resources(id) ON DELETE CASCADE,
    PRIMARY KEY (page_id, resource_id)
);

CREATE INDEX IF NOT EXISTS idx_page_resources_page ON page_resources(page_id);
