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

func TestGetResourceByURLPath_EscapesPercentWildcards(t *testing.T) {
	db := skipIfNoDB(t)
	defer db.Close()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	pageID, err := db.CreatePage(
		"https://db-url-path.example.com/page-"+suffix,
		"URL Path Escape",
		"html/test/url-path-escape.html",
		strings.Repeat("a", 64),
		time.Now(),
	)
	if err != nil {
		t.Fatalf("CreatePage failed: %v", err)
	}
	defer db.DeletePage(pageID)

	correctURL := "https://db-url-path.example.com/assets/report%20done.png?token=good"
	wrongURL := "https://db-url-path.example.com/assets/reportX20done.png?token=bad"

	correctID, err := db.CreateResource(correctURL, strings.Repeat("b", 64), "image", "resources/test/report-correct.img", 10)
	if err != nil {
		t.Fatalf("CreateResource(correct) failed: %v", err)
	}
	wrongID, err := db.CreateResource(wrongURL, strings.Repeat("c", 64), "image", "resources/test/report-wrong.img", 10)
	if err != nil {
		t.Fatalf("CreateResource(wrong) failed: %v", err)
	}

	for _, resourceID := range []int64{correctID, wrongID} {
		if err := db.LinkPageResource(pageID, resourceID); err != nil {
			t.Fatalf("LinkPageResource(%d) failed: %v", resourceID, err)
		}
	}

	resource, err := db.GetResourceByURLPath("https://db-url-path.example.com/assets/report%20done.png", pageID)
	if err != nil {
		t.Fatalf("GetResourceByURLPath failed: %v", err)
	}
	if resource == nil {
		t.Fatal("expected matching resource, got nil")
	}
	if resource.ID != correctID {
		t.Fatalf("resource ID = %d, want %d (wrong wildcard match to %q)", resource.ID, correctID, wrongURL)
	}
}

func TestGetResourceByURLPrefix_EscapesUnderscoreWildcards(t *testing.T) {
	db := skipIfNoDB(t)
	defer db.Close()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	pageID, err := db.CreatePage(
		"https://db-url-prefix.example.com/page-"+suffix,
		"URL Prefix Escape",
		"html/test/url-prefix-escape.html",
		strings.Repeat("d", 64),
		time.Now(),
	)
	if err != nil {
		t.Fatalf("CreatePage failed: %v", err)
	}
	defer db.DeletePage(pageID)

	correctURL := "https://db-url-prefix.example.com/assets/icon_1.svg#section"
	wrongURL := "https://db-url-prefix.example.com/assets/iconA1.svg#section"

	correctID, err := db.CreateResource(correctURL, strings.Repeat("e", 64), "image", "resources/test/icon-correct.img", 10)
	if err != nil {
		t.Fatalf("CreateResource(correct) failed: %v", err)
	}
	wrongID, err := db.CreateResource(wrongURL, strings.Repeat("f", 64), "image", "resources/test/icon-wrong.img", 10)
	if err != nil {
		t.Fatalf("CreateResource(wrong) failed: %v", err)
	}

	for _, resourceID := range []int64{correctID, wrongID} {
		if err := db.LinkPageResource(pageID, resourceID); err != nil {
			t.Fatalf("LinkPageResource(%d) failed: %v", resourceID, err)
		}
	}

	resource, err := db.GetResourceByURLPrefix("https://db-url-prefix.example.com/assets/icon_1.svg", pageID)
	if err != nil {
		t.Fatalf("GetResourceByURLPrefix failed: %v", err)
	}
	if resource == nil {
		t.Fatal("expected matching resource, got nil")
	}
	if resource.ID != correctID {
		t.Fatalf("resource ID = %d, want %d (wrong wildcard match to %q)", resource.ID, correctID, wrongURL)
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

func TestFinalizePageCreate_UpdatesResourceLastSeen(t *testing.T) {
	db := skipIfNoDB(t)
	defer db.Close()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	pageID, err := db.CreatePage(
		"https://finalize-last-seen.example.com/page-"+suffix,
		"Pending Page",
		"html/test/finalize-last-seen.html",
		strings.Repeat("a", 64),
		time.Now(),
	)
	if err != nil {
		t.Fatalf("CreatePage failed: %v", err)
	}
	defer db.DeletePage(pageID)

	resourceID, err := db.CreateResource(
		"https://finalize-last-seen.example.com/style.css?"+suffix,
		strings.Repeat("b", 64),
		"css",
		"resources/test/finalize-last-seen.css",
		123,
	)
	if err != nil {
		t.Fatalf("CreateResource failed: %v", err)
	}
	defer db.conn.Exec("DELETE FROM resources WHERE id = $1", resourceID)

	before := time.Now().Add(-2 * time.Hour)
	if _, err := db.conn.Exec("UPDATE resources SET last_seen = $1 WHERE id = $2", before, resourceID); err != nil {
		t.Fatalf("seed last_seen failed: %v", err)
	}

	if err := db.FinalizePageCreate(pageID, []int64{resourceID}); err != nil {
		t.Fatalf("FinalizePageCreate failed: %v", err)
	}

	resource, err := db.GetResourceByID(resourceID)
	if err != nil {
		t.Fatalf("GetResourceByID failed: %v", err)
	}
	if resource == nil {
		t.Fatal("resource should exist after finalize")
	}
	if !resource.LastSeen.After(before) {
		t.Fatalf("resource last_seen = %v, want after %v", resource.LastSeen, before)
	}

	var count int
	if err := db.conn.QueryRow("SELECT COUNT(*) FROM page_resources WHERE page_id = $1 AND resource_id = $2", pageID, resourceID).Scan(&count); err != nil {
		t.Fatalf("page_resources count failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 page-resource link, got %d", count)
	}
}

func TestReplacePageSnapshot_UpdatesResourceLastSeen(t *testing.T) {
	db := skipIfNoDB(t)
	defer db.Close()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	pageID, err := db.CreatePage(
		"https://replace-last-seen.example.com/page-"+suffix,
		"Before Replace",
		"html/test/replace-last-seen-before.html",
		strings.Repeat("a", 64),
		time.Now(),
	)
	if err != nil {
		t.Fatalf("CreatePage failed: %v", err)
	}
	defer db.DeletePage(pageID)

	oldResourceID, err := db.CreateResource(
		"https://replace-last-seen.example.com/old.css?"+suffix,
		strings.Repeat("b", 64),
		"css",
		"resources/test/replace-last-seen-old.css",
		111,
	)
	if err != nil {
		t.Fatalf("CreateResource(old) failed: %v", err)
	}
	defer db.conn.Exec("DELETE FROM resources WHERE id = $1", oldResourceID)

	newResourceID, err := db.CreateResource(
		"https://replace-last-seen.example.com/new.css?"+suffix,
		strings.Repeat("c", 64),
		"css",
		"resources/test/replace-last-seen-new.css",
		222,
	)
	if err != nil {
		t.Fatalf("CreateResource(new) failed: %v", err)
	}
	defer db.conn.Exec("DELETE FROM resources WHERE id = $1", newResourceID)

	if err := db.LinkPageResource(pageID, oldResourceID); err != nil {
		t.Fatalf("LinkPageResource failed: %v", err)
	}

	before := time.Now().Add(-2 * time.Hour)
	if _, err := db.conn.Exec("UPDATE resources SET last_seen = $1 WHERE id = $2", before, newResourceID); err != nil {
		t.Fatalf("seed new resource last_seen failed: %v", err)
	}

	bodyText := "updated body text"
	if err := db.ReplacePageSnapshot(
		pageID,
		"html/test/replace-last-seen-after.html",
		strings.Repeat("d", 64),
		"After Replace",
		&bodyText,
		[]int64{newResourceID},
	); err != nil {
		t.Fatalf("ReplacePageSnapshot failed: %v", err)
	}

	resource, err := db.GetResourceByID(newResourceID)
	if err != nil {
		t.Fatalf("GetResourceByID failed: %v", err)
	}
	if resource == nil {
		t.Fatal("new resource should exist after replace")
	}
	if !resource.LastSeen.After(before) {
		t.Fatalf("new resource last_seen = %v, want after %v", resource.LastSeen, before)
	}

	linked, err := db.GetResourcesByPageID(pageID)
	if err != nil {
		t.Fatalf("GetResourcesByPageID failed: %v", err)
	}
	if len(linked) != 1 {
		t.Fatalf("expected 1 linked resource after replace, got %d", len(linked))
	}
	if linked[0].ID != newResourceID {
		t.Fatalf("linked resource ID = %d, want %d", linked[0].ID, newResourceID)
	}
}
