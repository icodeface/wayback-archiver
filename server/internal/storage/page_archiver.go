package storage

import (
	"fmt"

	"wayback/internal/models"
)

type PageArchiver struct {
	process      func(*models.CaptureRequest) (int64, string, error)
	processAsync func(*models.CaptureRequest) (int64, string, error)
	update       func(int64, *models.CaptureRequest) (string, error)
	updateAsync  func(int64, *models.CaptureRequest) (string, error)
}

func NewPageArchiver(dedup *Deduplicator) *PageArchiver {
	if dedup == nil {
		return &PageArchiver{}
	}
	return &PageArchiver{process: dedup.processCapture, processAsync: dedup.processCaptureAsync, update: dedup.updateCaptureSync, updateAsync: dedup.updateCaptureAsync}
}

func (p *PageArchiver) Process(req *models.CaptureRequest) (int64, string, error) {
	if p == nil || p.process == nil {
		return 0, "", fmt.Errorf("page archiver is not configured")
	}
	return p.process(req)
}
func (p *PageArchiver) ProcessAsync(req *models.CaptureRequest) (int64, string, error) {
	if p == nil || p.processAsync == nil {
		return 0, "", fmt.Errorf("page archiver is not configured")
	}
	return p.processAsync(req)
}
func (p *PageArchiver) Update(pageID int64, req *models.CaptureRequest) (string, error) {
	if p == nil || p.update == nil {
		return "", fmt.Errorf("page archiver is not configured")
	}
	return p.update(pageID, req)
}
func (p *PageArchiver) UpdateAsync(pageID int64, req *models.CaptureRequest) (string, error) {
	if p == nil || p.updateAsync == nil {
		return "", fmt.Errorf("page archiver is not configured")
	}
	return p.updateAsync(pageID, req)
}
