package storage

import (
	"errors"
	"fmt"
	"log"
	"net"
	"regexp"
	"time"

	"wayback/internal/models"
)

type ResourceDownloader struct {
	storage     *FileStorage
	concurrency *ConcurrencyManager
	retries     int
}

type ResourceDownloadRequest struct {
	URL             string
	PageURL         string
	Headers         map[string]string
	Cookies         []models.CaptureCookie
	StreamThreshold int64
	ETag            string
	LastModified    string
}

type ResourceMetadata struct {
	ETag         string
	LastModified string
	FreshUntil   time.Time
	HasFreshness bool
	NotModified  bool
}
type ResourceDownloadTrace struct {
	Validation  time.Duration
	Request     time.Duration
	Body        time.Duration
	Mode        string
	StatusCode  int
	ContentSize int64
}
type ResourceDownloadResult struct {
	Data     []byte
	Hash     string
	TempPath string
	Metadata ResourceMetadata
	Trace    ResourceDownloadTrace
}

func NewResourceDownloader(storage *FileStorage, concurrency *ConcurrencyManager) *ResourceDownloader {
	return &ResourceDownloader{storage: storage, concurrency: concurrency, retries: 2}
}

func (d *ResourceDownloader) Download(req ResourceDownloadRequest) (result ResourceDownloadResult, err error) {
	if d == nil || d.storage == nil {
		return ResourceDownloadResult{}, fmt.Errorf("resource downloader is not configured")
	}
	for attempt := 0; attempt <= d.retries; attempt++ {
		unlock := d.concurrency.AcquireDownload()
		data, hash, tempPath, metadata, trace, downloadErr := d.storage.DownloadResourceWithMetadata(req.URL, req.PageURL, req.Headers, req.Cookies, req.StreamThreshold, req.ETag, req.LastModified)
		unlock()
		result = ResourceDownloadResult{Data: data, Hash: hash, TempPath: tempPath, Metadata: ResourceMetadata{ETag: metadata.etag, LastModified: metadata.lastMod, FreshUntil: metadata.freshUntil, HasFreshness: metadata.hasFreshness, NotModified: metadata.notModified}, Trace: ResourceDownloadTrace{Validation: trace.validate, Request: trace.request, Body: trace.body, Mode: trace.mode, StatusCode: trace.statusCode, ContentSize: trace.contentSize}}
		err = downloadErr
		if err == nil || attempt == d.retries || !isRetryableDownloadError(err) {
			return
		}
		log.Printf("[resource] download retry %d/%d url=%s err=%v", attempt+1, d.retries, shortURLForLog(req.URL), err)
	}
	return
}

var retryableHTTPStatusRe = regexp.MustCompile(`status (429|5\d\d)\b`)

func isRetryableDownloadError(err error) bool {
	if err == nil {
		return false
	}
	var networkErr net.Error
	return errors.As(err, &networkErr) || retryableHTTPStatusRe.MatchString(err.Error())
}
