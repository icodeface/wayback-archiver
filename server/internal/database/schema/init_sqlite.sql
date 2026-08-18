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

-- 资源表（去重）
CREATE TABLE IF NOT EXISTS resources (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    url TEXT NOT NULL,
    content_hash CHAR(64) NOT NULL,
    resource_type VARCHAR(20),
    file_path TEXT NOT NULL,
    file_size BIGINT,
    first_seen DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_seen DATETIME DEFAULT CURRENT_TIMESTAMP,
    is_quarantined BOOLEAN NOT NULL DEFAULT 0,
    quarantine_reason TEXT NOT NULL DEFAULT ''
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

-- 公开分享表（token_hash 存储 token 的哈希，不保存 token 明文）
CREATE TABLE IF NOT EXISTS page_shares (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    token_hash TEXT NOT NULL UNIQUE,
    page_id INTEGER REFERENCES pages(id) ON DELETE CASCADE,
    url TEXT NOT NULL,
    title TEXT,
    html_path TEXT NOT NULL,
    content_hash CHAR(64),
    captured_at DATETIME NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at DATETIME,
    revoked_at DATETIME,
    allow_markdown BOOLEAN NOT NULL DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_page_shares_page ON page_shares(page_id);
CREATE INDEX IF NOT EXISTS idx_page_shares_html_path ON page_shares(html_path);

CREATE TABLE IF NOT EXISTS page_share_resources (
    token_hash TEXT NOT NULL REFERENCES page_shares(token_hash) ON DELETE CASCADE,
    resource_id INTEGER NOT NULL REFERENCES resources(id) ON DELETE CASCADE,
    PRIMARY KEY (token_hash, resource_id)
);

CREATE INDEX IF NOT EXISTS idx_page_share_resources_resource ON page_share_resources(resource_id);

CREATE TABLE IF NOT EXISTS favorites (
    page_id INTEGER NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (page_id)
);

CREATE INDEX IF NOT EXISTS idx_favorites_created_at ON favorites(created_at DESC);
