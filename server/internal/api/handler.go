package api

import (
	"wayback/internal/database"
	"wayback/internal/logging"
	"wayback/internal/storage"
)

type Handler struct {
	dedup   *storage.Deduplicator
	db      database.Database
	css     *storage.CSSParser
	dataDir string
	logger  *logging.Logger
}

func NewHandler(dedup *storage.Deduplicator, db database.Database, dataDir string, logger *logging.Logger) *Handler {
	return &Handler{
		dedup:   dedup,
		db:      db,
		css:     storage.NewCSSParser(),
		dataDir: dataDir,
		logger:  logger,
	}
}

func (h *Handler) cssParser() *storage.CSSParser {
	if h.css == nil {
		h.css = storage.NewCSSParser()
	}
	return h.css
}
