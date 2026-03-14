package models

import "time"

type Page struct {
	ID           int64     `json:"id"`
	URL          string    `json:"url"`
	Title        string    `json:"title"`
	CapturedAt   time.Time `json:"captured_at"`
	HTMLPath     string    `json:"html_path"`
	ContentHash  string    `json:"content_hash"`
	FirstVisited time.Time `json:"first_visited"`
	LastVisited  time.Time `json:"last_visited"`
	BodyText     string    `json:"body_text,omitempty"`
	Domain       string    `json:"domain,omitempty"`
}

type Resource struct {
	ID           int64     `json:"id"`
	URL          string    `json:"url"`
	ContentHash  string    `json:"content_hash"`
	ResourceType string    `json:"resource_type"`
	FilePath     string    `json:"file_path"`
	FileSize     int64     `json:"file_size"`
	FirstSeen    time.Time `json:"first_seen"`
	LastSeen     time.Time `json:"last_seen"`
}

type CaptureRequest struct {
	URL       string              `json:"url" binding:"required"`
	Title     string              `json:"title"`
	HTML      string              `json:"html" binding:"required"`
	Resources []ResourceReference `json:"resources"`
	Headers   map[string]string   `json:"headers"`
}

type ResourceReference struct {
	Type    string `json:"type"`
	URL     string `json:"url"`
	Content string `json:"content"` // base64 编码的内容（可选）
}

const (
	ArchiveActionCreated   = "created"
	ArchiveActionUnchanged = "unchanged"
	ArchiveActionUpdated   = "updated"
)

type ArchiveResponse struct {
	Status string `json:"status"`
	PageID int64  `json:"page_id"`
	Action string `json:"action"`
}
