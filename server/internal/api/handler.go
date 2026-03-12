package api

import (
	"wayback/internal/database"
	"wayback/internal/logging"
	"wayback/internal/storage"
)

type Handler struct {
	dedup   *storage.Deduplicator
	db      *database.DB
	dataDir string
	logger  *logging.Logger
}

func NewHandler(dedup *storage.Deduplicator, db *database.DB, dataDir string, logger *logging.Logger) *Handler {
	return &Handler{
		dedup:   dedup,
		db:      db,
		dataDir: dataDir,
		logger:  logger,
	}
}
