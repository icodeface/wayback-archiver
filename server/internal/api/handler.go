package api

import (
	"wayback/internal/config"
	"wayback/internal/database"
	"wayback/internal/logging"
	"wayback/internal/storage"
)

type Handler struct {
	dedup    *storage.Deduplicator
	archiver *storage.PageArchiver
	db       database.Database
	css      *storage.CSSParser
	dataDir  string
	logger   *logging.Logger
	authCfg  *config.AuthConfig
}

func NewHandler(dedup *storage.Deduplicator, db database.Database, dataDir string, logger *logging.Logger, authCfg *config.AuthConfig) *Handler {
	var archiver *storage.PageArchiver
	if dedup != nil {
		archiver = dedup.PageArchiver()
	}
	return &Handler{
		dedup:    dedup,
		archiver: archiver,
		db:       db,
		css:      storage.NewCSSParser(),
		dataDir:  dataDir,
		logger:   logger,
		authCfg:  authCfg,
	}
}

// resourceCacheControl 返回归档资源的缓存策略。
//
// 认证开启时用 private（只允许浏览器缓存，禁止 CDN 跨用户共享），
// 认证关闭时用 public（允许 CDN 缓存）。
//
// 两者都设 1 年 max-age，浏览器缓存性能无差异。
func (h *Handler) resourceCacheControl() string {
	if h.authCfg != nil && h.authCfg.Enabled() {
		return "private, max-age=31536000"
	}
	return "public, max-age=31536000"
}

func (h *Handler) cssParser() *storage.CSSParser {
	if h.css == nil {
		h.css = storage.NewCSSParser()
	}
	return h.css
}
