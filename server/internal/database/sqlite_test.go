package database

import (
	"path/filepath"
	"testing"
	"time"
)

func newSQLiteTestDB(t *testing.T) *SQLiteDB {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "wayback.db")
	database, err := NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("NewSQLite failed: %v", err)
	}

	sqliteDB, ok := database.(*SQLiteDB)
	if !ok {
		t.Fatalf("expected *SQLiteDB, got %T", database)
	}

	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	return sqliteDB
}

func TestSQLiteFTSUpdatePageBodyText(t *testing.T) {
	db := newSQLiteTestDB(t)

	now := time.Now().UTC()
	pageID, err := db.CreatePage("https://example.com/fts-update", "Original Title", "html/test/original.html", "hash-1", now)
	if err != nil {
		t.Fatalf("CreatePage failed: %v", err)
	}

	if err := db.UpdatePageBodyText(pageID, "hello archived world"); err != nil {
		t.Fatalf("UpdatePageBodyText failed: %v", err)
	}

	pages, err := db.SearchPages("archived", nil, nil, "")
	if err != nil {
		t.Fatalf("SearchPages failed: %v", err)
	}
	if len(pages) != 1 || pages[0].ID != pageID {
		t.Fatalf("SearchPages returned %+v, want page %d", pages, pageID)
	}
}

func TestSQLiteSearchPages_MatchesURLTitleAndBodyText(t *testing.T) {
	db := newSQLiteTestDB(t)

	now := time.Now().UTC()
	pageID, err := db.CreatePage("https://test-update-feature.example.com/page-1", "Update Test - Original", "html/test/original.html", "hash-search", now)
	if err != nil {
		t.Fatalf("CreatePage failed: %v", err)
	}
	if err := db.UpdatePageBodyText(pageID, "This archived body includes dynamic content."); err != nil {
		t.Fatalf("UpdatePageBodyText failed: %v", err)
	}

	tests := []struct {
		name    string
		keyword string
	}{
		{name: "URL substring", keyword: "test-update-feature.example.com"},
		{name: "title substring", keyword: "Update Test - Original"},
		{name: "body text substring", keyword: "dynamic content"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pages, err := db.SearchPages(tt.keyword, nil, nil, "")
			if err != nil {
				t.Fatalf("SearchPages failed: %v", err)
			}
			if len(pages) != 1 || pages[0].ID != pageID {
				t.Fatalf("SearchPages(%q) returned %+v, want page %d", tt.keyword, pages, pageID)
			}
		})
	}
}

func TestSQLiteReplacePageSnapshotWithBodyText(t *testing.T) {
	db := newSQLiteTestDB(t)

	now := time.Now().UTC()
	pageID, err := db.CreatePage("https://example.com/replace", "Original Title", "html/test/original.html", "hash-1", now)
	if err != nil {
		t.Fatalf("CreatePage failed: %v", err)
	}

	bodyText := "snapshot body text"
	if err := db.ReplacePageSnapshot(pageID, "html/test/updated.html", "hash-2", "Updated Title", &bodyText, nil); err != nil {
		t.Fatalf("ReplacePageSnapshot failed: %v", err)
	}

	page, err := db.GetPageByID(idStr(pageID))
	if err != nil {
		t.Fatalf("GetPageByID failed: %v", err)
	}
	if page == nil {
		t.Fatal("page should exist after ReplacePageSnapshot")
	}
	if page.HTMLPath != "html/test/updated.html" {
		t.Fatalf("HTMLPath = %q, want %q", page.HTMLPath, "html/test/updated.html")
	}
	if page.ContentHash != "hash-2" {
		t.Fatalf("ContentHash = %q, want %q", page.ContentHash, "hash-2")
	}
	if page.Title != "Updated Title" {
		t.Fatalf("Title = %q, want %q", page.Title, "Updated Title")
	}

	pages, err := db.SearchPages("snapshot", nil, nil, "")
	if err != nil {
		t.Fatalf("SearchPages failed: %v", err)
	}
	if len(pages) != 1 || pages[0].ID != pageID {
		t.Fatalf("SearchPages returned %+v, want page %d", pages, pageID)
	}
}
