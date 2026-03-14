-- Wayback 数据库初始化脚本

CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- 页面归档表
CREATE TABLE pages (
    id BIGSERIAL PRIMARY KEY,
    url TEXT NOT NULL,
    title TEXT,
    captured_at TIMESTAMP WITH TIME ZONE NOT NULL,
    html_path TEXT NOT NULL,
    content_hash CHAR(64),
    first_visited TIMESTAMP WITH TIME ZONE,
    last_visited TIMESTAMP WITH TIME ZONE,
    body_text TEXT,
    domain TEXT DEFAULT ''
);

CREATE INDEX idx_pages_url ON pages(url);
CREATE INDEX idx_pages_captured_at ON pages(captured_at DESC);
CREATE INDEX idx_pages_url_time ON pages(url, captured_at DESC);
CREATE INDEX idx_pages_content_hash ON pages(content_hash);
CREATE INDEX idx_pages_url_hash ON pages(url, content_hash);
CREATE INDEX idx_pages_body_text_trgm ON pages USING gin (body_text gin_trgm_ops);
CREATE INDEX idx_pages_title_trgm ON pages USING gin (title gin_trgm_ops);
CREATE INDEX idx_pages_domain ON pages(domain);

-- 资源表（去重）
CREATE TABLE resources (
    id BIGSERIAL PRIMARY KEY,
    url TEXT NOT NULL,
    content_hash CHAR(64) NOT NULL,
    resource_type VARCHAR(20),
    file_path TEXT NOT NULL,
    file_size BIGINT,
    first_seen TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    last_seen TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX idx_resources_hash ON resources(content_hash);
CREATE INDEX idx_resources_url ON resources(url);

-- 页面-资源关联表
CREATE TABLE page_resources (
    page_id BIGINT REFERENCES pages(id) ON DELETE CASCADE,
    resource_id BIGINT REFERENCES resources(id) ON DELETE CASCADE,
    PRIMARY KEY (page_id, resource_id)
);

CREATE INDEX idx_page_resources_page ON page_resources(page_id);
