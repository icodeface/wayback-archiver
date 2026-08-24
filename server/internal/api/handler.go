package api

import (
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
}

func NewHandler(dedup *storage.Deduplicator, db database.Database, dataDir string, logger *logging.Logger) *Handler {
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
	}
}

func (h *Handler) cssParser() *storage.CSSParser {
	if h.css == nil {
		h.css = storage.NewCSSParser()
	}
	return h.css
}
