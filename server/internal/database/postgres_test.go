package database

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestBuildConnectionString_DefaultSSLMode(t *testing.T) {
	connStr := buildConnectionString("localhost", "5432", "postgres", "", "wayback", "disable")

	for _, want := range []string{"host='localhost'", "port='5432'", "dbname='wayback'", "user='postgres'", "sslmode='disable'"} {
		if !strings.Contains(connStr, want) {
			t.Fatalf("connection string %q missing %q", connStr, want)
		}
	}
}

func TestBuildConnectionString_CustomSSLMode(t *testing.T) {
	connStr := buildConnectionString("db.internal", "5432", "app", "secret value", "wayback", "require")

	for _, want := range []string{"host='db.internal'", "user='app'", "password='secret value'", "sslmode='require'"} {
		if !strings.Contains(connStr, want) {
			t.Fatalf("connection string %q missing %q", connStr, want)
		}
	}
}

// skipIfNoDB connects to the test database or skips the test.
func skipIfNoDB(t *testing.T) *DB {
	t.Helper()
	db, err := New("localhost", "5432", "postgres", "", "wayback")
	if err != nil {
		t.Skipf("Skipping DB test (cannot connect): %v", err)
	}
	return db
}

func idStr(id int64) string {
	return fmt.Sprintf("%d", id)
}

func TestUpdatePageContent(t *testing.T) {
	db := skipIfNoDB(t)
	defer db.Close()

	now := time.Now()
	origHash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	newHash := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	pageID, err := db.CreatePage("http://test-update-content.example.com", "Original Title", "html/test/original.html", origHash, now)
	if err != nil {
		t.Fatalf("CreatePage failed: %v", err)
	}
	defer db.DeletePage(pageID)

	// Update content
	err = db.UpdatePageContent(pageID, "html/test/updated.html", newHash, "Updated Title")
	if err != nil {
		t.Fatalf("UpdatePageContent failed: %v", err)
	}

	// Verify
	page, err := db.GetPageByID(idStr(pageID))
	if err != nil {
		t.Fatalf("GetPageByID failed: %v", err)
	}
	if page == nil {
		t.Fatal("page should exist after update")
	}
	if page.HTMLPath != "html/test/updated.html" {
		t.Errorf("HTMLPath = %q, want %q", page.HTMLPath, "html/test/updated.html")
	}
	if page.ContentHash != newHash {
		t.Errorf("ContentHash = %q, want %q", page.ContentHash, newHash)
	}
	if page.Title != "Updated Title" {
		t.Errorf("Title = %q, want %q", page.Title, "Updated Title")
	}
	if !page.LastVisited.After(now) {
		t.Errorf("LastVisited should be updated to after creation time")
	}
}

func TestDeletePageResources(t *testing.T) {
	db := skipIfNoDB(t)
	defer db.Close()

	now := time.Now()
	pageID, err := db.CreatePage("http://test-delete-resources.example.com", "Test", "html/test/del.html", "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", now)
	if err != nil {
		t.Fatalf("CreatePage failed: %v", err)
	}
	defer db.DeletePage(pageID)

	// Create a resource and link it
	resID, err := db.CreateResource("http://test-delete-resources.example.com/style.css", "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd", "css", "resources/dd/dd/dddd.css", 100)
	if err != nil {
		t.Fatalf("CreateResource failed: %v", err)
	}

	err = db.LinkPageResource(pageID, resID)
	if err != nil {
		t.Fatalf("LinkPageResource failed: %v", err)
	}

	// Verify link exists via count
	var count int
	err = db.conn.QueryRow("SELECT COUNT(*) FROM page_resources WHERE page_id = $1", pageID).Scan(&count)
	if err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 linked resource, got %d", count)
	}

	// Delete page resources
	err = db.DeletePageResources(pageID)
	if err != nil {
		t.Fatalf("DeletePageResources failed: %v", err)
	}

	// Verify links are gone
	err = db.conn.QueryRow("SELECT COUNT(*) FROM page_resources WHERE page_id = $1", pageID).Scan(&count)
	if err != nil {
		t.Fatalf("count query after delete failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 linked resources after delete, got %d", count)
	}

	// Verify the resource record itself still exists
	res, err := db.GetResourceByID(resID)
	if err != nil {
		t.Fatalf("GetResourceByID failed: %v", err)
	}
	if res == nil {
		t.Error("resource record should still exist after DeletePageResources")
	}

	// Cleanup resource
	db.conn.Exec("DELETE FROM resources WHERE id = $1", resID)
}

func TestUpdatePageContent_NonExistentPage(t *testing.T) {
	db := skipIfNoDB(t)
	defer db.Close()

	// UPDATE on non-existent row affects 0 rows — should not error
	err := db.UpdatePageContent(999999999, "html/test/nope.html", "zzzz", "Nope")
	if err != nil {
		t.Fatalf("UpdatePageContent on non-existent page should not error, got: %v", err)
	}
}

func TestDeletePageResources_NoLinks(t *testing.T) {
	db := skipIfNoDB(t)
	defer db.Close()

	err := db.DeletePageResources(999999999)
	if err != nil {
		t.Fatalf("DeletePageResources on page with no links should not error, got: %v", err)
	}
}

func TestGetPagesByURL(t *testing.T) {
	db := skipIfNoDB(t)
	defer db.Close()

	now := time.Now()
	testURL := "http://test-get-pages-by-url.example.com"

	// Create 3 snapshots with different content hashes
	hash1 := "1111111111111111111111111111111111111111111111111111111111111111"
	hash2 := "2222222222222222222222222222222222222222222222222222222222222222"
	hash3 := "3333333333333333333333333333333333333333333333333333333333333333"

	id1, err := db.CreatePage(testURL, "Snapshot 1", "html/test/snap1.html", hash1, now.Add(-2*time.Hour))
	if err != nil {
		t.Fatalf("CreatePage 1 failed: %v", err)
	}
	defer db.DeletePage(id1)

	id2, err := db.CreatePage(testURL, "Snapshot 2", "html/test/snap2.html", hash2, now.Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("CreatePage 2 failed: %v", err)
	}
	defer db.DeletePage(id2)

	id3, err := db.CreatePage(testURL, "Snapshot 3", "html/test/snap3.html", hash3, now)
	if err != nil {
		t.Fatalf("CreatePage 3 failed: %v", err)
	}
	defer db.DeletePage(id3)

	// Query all snapshots
	pages, err := db.GetPagesByURL(testURL)
	if err != nil {
		t.Fatalf("GetPagesByURL failed: %v", err)
	}
	if len(pages) != 3 {
		t.Fatalf("expected 3 snapshots, got %d", len(pages))
	}

	// Should be ordered by first_visited DESC (newest first)
	if pages[0].ID != id3 {
		t.Errorf("first snapshot should be id3 (%d), got %d", id3, pages[0].ID)
	}
	if pages[2].ID != id1 {
		t.Errorf("last snapshot should be id1 (%d), got %d", id1, pages[2].ID)
	}
}

func TestGetPagesByURL_NoResults(t *testing.T) {
	db := skipIfNoDB(t)
	defer db.Close()

	pages, err := db.GetPagesByURL("http://nonexistent-url-test-12345.example.com")
	if err != nil {
		t.Fatalf("GetPagesByURL should not error for missing URL: %v", err)
	}
	if len(pages) != 0 {
		t.Errorf("expected 0 snapshots, got %d", len(pages))
	}
}

func TestGetSnapshotNeighbors(t *testing.T) {
	db := skipIfNoDB(t)
	defer db.Close()

	now := time.Now()
	testURL := "http://test-snapshot-neighbors.example.com"

	hash1 := "aaaa111111111111111111111111111111111111111111111111111111111111"
	hash2 := "aaaa222222222222222222222222222222222222222222222222222222222222"
	hash3 := "aaaa333333333333333333333333333333333333333333333333333333333333"

	id1, err := db.CreatePage(testURL, "Snap A", "html/test/a.html", hash1, now.Add(-2*time.Hour))
	if err != nil {
		t.Fatalf("CreatePage 1 failed: %v", err)
	}
	defer db.DeletePage(id1)

	id2, err := db.CreatePage(testURL, "Snap B", "html/test/b.html", hash2, now.Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("CreatePage 2 failed: %v", err)
	}
	defer db.DeletePage(id2)

	id3, err := db.CreatePage(testURL, "Snap C", "html/test/c.html", hash3, now)
	if err != nil {
		t.Fatalf("CreatePage 3 failed: %v", err)
	}
	defer db.DeletePage(id3)

	// Middle snapshot: should have both prev and next
	prev, next, total, err := db.GetSnapshotNeighbors(testURL, id2)
	if err != nil {
		t.Fatalf("GetSnapshotNeighbors failed: %v", err)
	}
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	if prev == nil {
		t.Fatal("prev should not be nil for middle snapshot")
	}
	if prev.ID != id1 {
		t.Errorf("prev.ID = %d, want %d", prev.ID, id1)
	}
	if next == nil {
		t.Fatal("next should not be nil for middle snapshot")
	}
	if next.ID != id3 {
		t.Errorf("next.ID = %d, want %d", next.ID, id3)
	}

	// Oldest snapshot: no prev, has next
	prev, next, _, err = db.GetSnapshotNeighbors(testURL, id1)
	if err != nil {
		t.Fatalf("GetSnapshotNeighbors for oldest failed: %v", err)
	}
	if prev != nil {
		t.Errorf("oldest snapshot should have no prev, got ID %d", prev.ID)
	}
	if next == nil || next.ID != id2 {
		t.Errorf("oldest snapshot next should be id2 (%d)", id2)
	}

	// Newest snapshot: has prev, no next
	prev, next, _, err = db.GetSnapshotNeighbors(testURL, id3)
	if err != nil {
		t.Fatalf("GetSnapshotNeighbors for newest failed: %v", err)
	}
	if prev == nil || prev.ID != id2 {
		t.Errorf("newest snapshot prev should be id2 (%d)", id2)
	}
	if next != nil {
		t.Errorf("newest snapshot should have no next, got ID %d", next.ID)
	}
}
