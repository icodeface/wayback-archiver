package database

import (
	"time"
	"wayback/internal/models"
)

// Database 定义所有数据库操作的接口
// 支持 PostgreSQL 和 SQLite 两种实现
type Database interface {
	// 连接管理
	Close() error

	// 页面操作
	CreatePage(url, title, htmlPath, contentHash string, capturedAt time.Time) (int64, error)
	GetPageByID(id string) (*models.Page, error)
	GetPageByURLAndHash(url, contentHash string) (*models.Page, error)
	GetPagesByURL(pageURL string) ([]models.Page, error)
	ListPages(limit, offset int, from, to *time.Time, domain string) ([]models.Page, error)
	GetTotalPagesCount(from, to *time.Time, domain string) (int, error)
	SearchPages(keyword string, from, to *time.Time, domain string) ([]models.Page, error)
	GetPagesWithoutBodyText() ([]models.Page, error)
	GetSnapshotNeighbors(pageURL string, currentID int64) (prev *models.Page, next *models.Page, total int, err error)

	UpdatePageBodyText(id int64, bodyText string) error
	UpdatePageLastVisited(id int64, lastVisited time.Time) error
	UpdatePageContent(id int64, htmlPath, contentHash, title string) error
	ReplacePageSnapshot(id int64, htmlPath, contentHash, title string, bodyText *string, resourceIDs []int64) error
	ResetPageForCreateRetry(id int64, title, htmlPath string, capturedAt time.Time) (string, error)
	FinalizePageCreate(id int64, resourceIDs []int64) error
	MarkPageCreateFailed(id int64) error
	DeletePage(id int64) error
	CheckRecentCapture(url string, within time.Duration) (bool, error)

	// 资源操作
	CreateResource(url, hash, resourceType, filePath string, fileSize int64) (int64, error)
	GetResourceByID(id int64) (*models.Resource, error)
	GetResourceByHash(hash string) (*models.Resource, error)
	GetResourceByURL(url string) (*models.Resource, error)
	GetResourceByURLLike(pattern string) (*models.Resource, error)
	GetResourceByURLAndPageID(url string, pageID int64) (*models.Resource, error)
	GetLinkedResourceByURLAndPageID(url string, pageID int64) (*models.Resource, error)
	GetResourceByURLPrefix(urlPrefix string, pageID int64) (*models.Resource, error)
	GetResourceByURLPath(urlPath string, pageID int64) (*models.Resource, error)
	GetResourcesByPageID(pageID int64) ([]models.Resource, error)
	ListResourcesForIntegrityCheck(resourceType string, lastID int64, limit int) ([]models.Resource, error)
	UpdateResourceLastSeen(id int64) error
	QuarantineResourcesByFilePath(filePath, quarantinePath, reason string) (int64, error)

	// 页面-资源关联
	LinkPageResource(pageID, resourceID int64) error
	LinkPageResources(pageID int64, resourceIDs []int64) error
	DeletePageResources(pageID int64) error
}
