package storage

import (
	"fmt"

	"wayback/internal/models"
)

type HTMLRewriteRequest struct {
	HTML      string
	BaseURL   string
	Headers   map[string]string
	Cookies   []models.CaptureCookie
	Frames    []models.FrameCapture
	PageID    int64
	Timestamp string
}
type HTMLRewriteResult struct {
	HTML        string
	ResourceIDs []int64
}

type HTMLRewriter struct {
	process func(HTMLRewriteRequest) (HTMLRewriteResult, error)
}

func NewHTMLRewriter(process func(HTMLRewriteRequest) (HTMLRewriteResult, error)) *HTMLRewriter {
	return &HTMLRewriter{process: process}
}
func (r *HTMLRewriter) Rewrite(req HTMLRewriteRequest) (HTMLRewriteResult, error) {
	if r == nil || r.process == nil {
		return HTMLRewriteResult{}, fmt.Errorf("html rewriter is not configured")
	}
	return r.process(req)
}
