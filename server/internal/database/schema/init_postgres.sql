-- Wayback 数据库初始化脚本（PostgreSQL）

-- 页面归档表
CREATE TABLE IF NOT EXISTS pages (
    id BIGSERIAL PRIMARY KEY,
    url TEXT NOT NULL,
    title TEXT,
    captured_at TIMESTAMP WITH TIME ZONE NOT NULL,
    html_path TEXT NOT NULL,
    content_hash CHAR(64),
    snapshot_state VARCHAR(16) NOT NULL DEFAULT 'pending',
    first_visited TIMESTAMP WITH TIME ZONE,
    last_visited TIMESTAMP WITH TIME ZONE,
    body_text TEXT,
    domain TEXT DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_pages_url ON pages(url);
CREATE INDEX IF NOT EXISTS idx_pages_captured_at ON pages(captured_at DESC);
CREATE INDEX IF NOT EXISTS idx_pages_url_time ON pages(url, captured_at DESC);
CREATE INDEX IF NOT EXISTS idx_pages_content_hash ON pages(content_hash);
CREATE INDEX IF NOT EXISTS idx_pages_url_hash ON pages(url, content_hash);
CREATE INDEX IF NOT EXISTS idx_pages_domain ON pages(domain);
CREATE INDEX IF NOT EXISTS idx_pages_activity_desc ON pages ((COALESCE(last_visited, captured_at)) DESC, id DESC);

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pg_trgm') THEN
        EXECUTE 'CREATE INDEX IF NOT EXISTS idx_pages_search_text_trgm ON pages USING gin ((COALESCE(url, '''') || E''\n'' || COALESCE(title, '''') || E''\n'' || COALESCE(body_text, '''')) gin_trgm_ops)';
    END IF;
END $$;

-- 资源表（去重）
CREATE TABLE IF NOT EXISTS resources (
    id BIGSERIAL PRIMARY KEY,
    url TEXT NOT NULL,
    content_hash CHAR(64) NOT NULL,
    resource_type VARCHAR(20),
    file_path TEXT NOT NULL,
    file_size BIGINT,
    first_seen TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    last_seen TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    is_quarantined BOOLEAN NOT NULL DEFAULT FALSE,
    quarantine_reason TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_resources_hash ON resources(content_hash);
CREATE INDEX IF NOT EXISTS idx_resources_url ON resources(url);

-- 页面-资源关联表
CREATE TABLE IF NOT EXISTS page_resources (
    page_id BIGINT REFERENCES pages(id) ON DELETE CASCADE,
    resource_id BIGINT REFERENCES resources(id) ON DELETE CASCADE,
    PRIMARY KEY (page_id, resource_id)
);

CREATE INDEX IF NOT EXISTS idx_page_resources_page ON page_resources(page_id);

-- 公开分享表（token_hash 存储 token 的哈希，不保存 token 明文）
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
