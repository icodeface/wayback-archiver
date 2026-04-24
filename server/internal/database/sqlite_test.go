package database

import (
	"path/filepath"
	"strings"
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

func TestSQLiteNewDBMarksCurrentMigrationVersion(t *testing.T) {
	db := newSQLiteTestDB(t)

	if got := sqliteUserVersion(t, db); got != sqliteMigrationVersionCurrent {
		t.Fatalf("user_version = %d, want %d", got, sqliteMigrationVersionCurrent)
	}
}

func TestSQLiteStartupHeavyMigrationRunsOnlyOnce(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "wayback.db")
	openDB := func() *SQLiteDB {
		t.Helper()

		database, err := NewSQLite(dbPath)
		if err != nil {
			t.Fatalf("NewSQLite failed: %v", err)
		}

		sqliteDB, ok := database.(*SQLiteDB)
		if !ok {
			t.Fatalf("expected *SQLiteDB, got %T", database)
		}

		return sqliteDB
	}

	db := openDB()
	pageID, err := db.CreatePage("https://example.com/legacy-migration", "Legacy Page", "html/test/legacy.html", "hash-legacy", time.Date(2026, 4, 24, 15, 23, 24, 230000000, time.FixedZone("CST", 8*3600)))
	if err != nil {
		t.Fatalf("CreatePage failed: %v", err)
	}
	resourceID, err := db.CreateResource("https://example.com/style.css", "hash-resource", "css", "assets/test/style.css", 123)
	if err != nil {
		t.Fatalf("CreateResource failed: %v", err)
	}

	if _, err := db.conn.Exec("UPDATE pages SET captured_at = ?, first_visited = ?, last_visited = ? WHERE id = ?", "2026-04-24 15:23:24.230476+08:00", "2026-04-24 15:23:24.230476+08:00", "2026-04-24 07:23:51", pageID); err != nil {
		t.Fatalf("seed mixed page timestamps failed: %v", err)
	}
	if _, err := db.conn.Exec("UPDATE resources SET first_seen = ?, last_seen = ? WHERE id = ?", "2026-04-24 15:23:24.230476+08:00", "2026-04-24 07:23:51", resourceID); err != nil {
		t.Fatalf("seed mixed resource timestamps failed: %v", err)
	}
	if _, err := db.conn.Exec("CREATE TABLE migration_audit (target TEXT PRIMARY KEY, count INTEGER NOT NULL DEFAULT 0)"); err != nil {
		t.Fatalf("create migration_audit failed: %v", err)
	}
	if _, err := db.conn.Exec("INSERT INTO migration_audit(target, count) VALUES ('pages', 0), ('resources', 0)"); err != nil {
		t.Fatalf("seed migration_audit failed: %v", err)
	}
	if _, err := db.conn.Exec("CREATE TRIGGER count_pages_updates AFTER UPDATE ON pages BEGIN UPDATE migration_audit SET count = count + 1 WHERE target = 'pages'; END;"); err != nil {
		t.Fatalf("create pages audit trigger failed: %v", err)
	}
	if _, err := db.conn.Exec("CREATE TRIGGER count_resources_updates AFTER UPDATE ON resources BEGIN UPDATE migration_audit SET count = count + 1 WHERE target = 'resources'; END;"); err != nil {
		t.Fatalf("create resources audit trigger failed: %v", err)
	}
	if err := db.setSQLiteUserVersion(0); err != nil {
		t.Fatalf("setSQLiteUserVersion failed: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	db = openDB()
	if got := sqliteUserVersion(t, db); got != sqliteMigrationVersionCurrent {
		t.Fatalf("user_version after legacy startup = %d, want %d", got, sqliteMigrationVersionCurrent)
	}
	if got := sqliteAuditCount(t, db, "pages"); got != 1 {
		t.Fatalf("pages updated %d times during first legacy startup, want 1", got)
	}
	if got := sqliteAuditCount(t, db, "resources"); got != 1 {
		t.Fatalf("resources updated %d times during first legacy startup, want 1", got)
	}

	var capturedAt, firstVisited, lastVisited string
	if err := db.conn.QueryRow("SELECT CAST(captured_at AS TEXT), CAST(first_visited AS TEXT), CAST(last_visited AS TEXT) FROM pages WHERE id = ?", pageID).Scan(&capturedAt, &firstVisited, &lastVisited); err != nil {
		t.Fatalf("query normalized page timestamps failed: %v", err)
	}
	var firstSeen, lastSeen string
	if err := db.conn.QueryRow("SELECT CAST(first_seen AS TEXT), CAST(last_seen AS TEXT) FROM resources WHERE id = ?", resourceID).Scan(&firstSeen, &lastSeen); err != nil {
		t.Fatalf("query normalized resource timestamps failed: %v", err)
	}

	for name, value := range map[string]string{
		"captured_at":   capturedAt,
		"first_visited": firstVisited,
		"last_visited":  lastVisited,
		"first_seen":    firstSeen,
		"last_seen":     lastSeen,
	} {
		assertSQLiteTimestampText(t, name, value)
	}

	if !sqliteTriggerExists(t, db, "pages_fts_update") {
		t.Fatal("pages_fts_update trigger should exist after legacy migration")
	}
	if _, err := db.conn.Exec("CREATE TRIGGER pages_block_update BEFORE UPDATE ON pages BEGIN SELECT RAISE(ABORT, 'unexpected pages update'); END;"); err != nil {
		t.Fatalf("create pages blocker trigger failed: %v", err)
	}
	if _, err := db.conn.Exec("CREATE TRIGGER resources_block_update BEFORE UPDATE ON resources BEGIN SELECT RAISE(ABORT, 'unexpected resources update'); END;"); err != nil {
		t.Fatalf("create resources blocker trigger failed: %v", err)
	}
	if _, err := db.conn.Exec("DROP TRIGGER pages_fts_update"); err != nil {
		t.Fatalf("drop pages_fts_update failed: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	db = openDB()
	defer db.Close()

	if got := sqliteUserVersion(t, db); got != sqliteMigrationVersionCurrent {
		t.Fatalf("user_version after second startup = %d, want %d", got, sqliteMigrationVersionCurrent)
	}
	if sqliteTriggerExists(t, db, "pages_fts_update") {
		t.Fatal("pages_fts_update was recreated on second startup")
	}
}

func TestSQLiteNormalizeTimestamps_UnifiesStoredFormat(t *testing.T) {
	db := newSQLiteTestDB(t)

	pageID, err := db.CreatePage("https://example.com/timestamp", "Timestamp Page", "html/test/timestamp.html", "hash-ts", time.Date(2026, 4, 24, 15, 23, 24, 230000000, time.FixedZone("CST", 8*3600)))
	if err != nil {
		t.Fatalf("CreatePage failed: %v", err)
	}

	if _, err := db.conn.Exec("UPDATE pages SET captured_at = ?, first_visited = ?, last_visited = ? WHERE id = ?", "2026-04-24 15:23:24.230476+08:00", "2026-04-24 15:23:24.230476+08:00", "2026-04-24 07:23:51", pageID); err != nil {
		t.Fatalf("seed mixed timestamp formats failed: %v", err)
	}

	if err := db.ensureNormalizedTimestamps(); err != nil {
		t.Fatalf("ensureNormalizedTimestamps failed: %v", err)
	}

	var capturedAt, firstVisited, lastVisited string
	if err := db.conn.QueryRow("SELECT CAST(captured_at AS TEXT), CAST(first_visited AS TEXT), CAST(last_visited AS TEXT) FROM pages WHERE id = ?", pageID).Scan(&capturedAt, &firstVisited, &lastVisited); err != nil {
		t.Fatalf("query normalized timestamps failed: %v", err)
	}

	for name, value := range map[string]string{
		"captured_at":   capturedAt,
		"first_visited": firstVisited,
		"last_visited":  lastVisited,
	} {
		assertSQLiteTimestampText(t, name, value)
	}
}

func TestSQLiteUpgradeFromVariablePrecisionTimestamps(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "wayback.db")
	openDB := func() *SQLiteDB {
		t.Helper()

		database, err := NewSQLite(dbPath)
		if err != nil {
			t.Fatalf("NewSQLite failed: %v", err)
		}

		sqliteDB, ok := database.(*SQLiteDB)
		if !ok {
			t.Fatalf("expected *SQLiteDB, got %T", database)
		}

		return sqliteDB
	}

	db := openDB()
	pageID, err := db.CreatePage("https://example.com/version-3-upgrade", "Version 3 Upgrade", "html/test/version3.html", strings.Repeat("a", 64), time.Now().UTC())
	if err != nil {
		t.Fatalf("CreatePage failed: %v", err)
	}
	resourceID, err := db.CreateResource("https://example.com/version-3.css", strings.Repeat("b", 64), "css", "assets/test/version-3.css", 123)
	if err != nil {
		t.Fatalf("CreateResource failed: %v", err)
	}

	if _, err := db.conn.Exec("UPDATE pages SET captured_at = ?, first_visited = ?, last_visited = ? WHERE id = ?", "2026-04-24T07:23:51Z", "2026-04-24T07:23:51.1Z", "2026-04-24T07:23:51.25Z", pageID); err != nil {
		t.Fatalf("seed variable-precision page timestamps failed: %v", err)
	}
	if _, err := db.conn.Exec("UPDATE resources SET first_seen = ?, last_seen = ? WHERE id = ?", "2026-04-24T07:23:51Z", "2026-04-24T07:23:51.1Z", resourceID); err != nil {
		t.Fatalf("seed variable-precision resource timestamps failed: %v", err)
	}
	if err := db.setSQLiteUserVersion(sqliteMigrationVersionTimestampNormalization); err != nil {
		t.Fatalf("setSQLiteUserVersion failed: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	db = openDB()
	defer db.Close()

	if got := sqliteUserVersion(t, db); got != sqliteMigrationVersionCurrent {
		t.Fatalf("user_version after version 3 upgrade = %d, want %d", got, sqliteMigrationVersionCurrent)
	}

	var capturedAt, firstVisited, lastVisited string
	if err := db.conn.QueryRow("SELECT CAST(captured_at AS TEXT), CAST(first_visited AS TEXT), CAST(last_visited AS TEXT) FROM pages WHERE id = ?", pageID).Scan(&capturedAt, &firstVisited, &lastVisited); err != nil {
		t.Fatalf("query upgraded page timestamps failed: %v", err)
	}
	var firstSeen, lastSeen string
	if err := db.conn.QueryRow("SELECT CAST(first_seen AS TEXT), CAST(last_seen AS TEXT) FROM resources WHERE id = ?", resourceID).Scan(&firstSeen, &lastSeen); err != nil {
		t.Fatalf("query upgraded resource timestamps failed: %v", err)
	}

	for name, value := range map[string]string{
		"captured_at":   capturedAt,
		"first_visited": firstVisited,
		"last_visited":  lastVisited,
		"first_seen":    firstSeen,
		"last_seen":     lastSeen,
	} {
		assertSQLiteTimestampText(t, name, value)
	}
}

func TestSQLiteListPages_OrdersByActualTimeAfterNormalization(t *testing.T) {
	db := newSQLiteTestDB(t)

	olderID, err := db.CreatePage("https://example.com/older", "Older", "html/test/older.html", "hash-older", time.Date(2026, 4, 24, 15, 20, 21, 0, time.FixedZone("CST", 8*3600)))
	if err != nil {
		t.Fatalf("CreatePage older failed: %v", err)
	}
	newerID, err := db.CreatePage("https://example.com/newer", "Newer", "html/test/newer.html", "hash-newer", time.Date(2026, 4, 24, 15, 23, 24, 0, time.FixedZone("CST", 8*3600)))
	if err != nil {
		t.Fatalf("CreatePage newer failed: %v", err)
	}

	if _, err := db.conn.Exec("UPDATE pages SET last_visited = ? WHERE id = ?", "2026-04-24 15:20:21.847822+08:00", olderID); err != nil {
		t.Fatalf("seed older last_visited failed: %v", err)
	}
	if _, err := db.conn.Exec("UPDATE pages SET last_visited = ? WHERE id = ?", "2026-04-24 07:23:51", newerID); err != nil {
		t.Fatalf("seed newer last_visited failed: %v", err)
	}

	if err := db.ensureNormalizedTimestamps(); err != nil {
		t.Fatalf("ensureNormalizedTimestamps failed: %v", err)
	}

	pages, err := db.ListPages(10, 0, nil, nil, "")
	if err != nil {
		t.Fatalf("ListPages failed: %v", err)
	}
	if len(pages) < 2 {
		t.Fatalf("ListPages returned %d pages, want at least 2", len(pages))
	}
	if pages[0].ID != newerID || pages[1].ID != olderID {
		t.Fatalf("ListPages order = [%d, %d], want [%d, %d]", pages[0].ID, pages[1].ID, newerID, olderID)
	}
}

func TestSQLiteFixedWidthTimestampFormat_SortsLexicographically(t *testing.T) {
	earlier := time.Date(2026, 4, 24, 7, 23, 51, 0, time.UTC)
	later := earlier.Add(100 * time.Millisecond)

	earlierText := formatSQLiteTimestamp(earlier)
	laterText := formatSQLiteTimestamp(later)

	if len(earlierText) != len(laterText) {
		t.Fatalf("timestamp lengths differ: %q (%d) vs %q (%d)", earlierText, len(earlierText), laterText, len(laterText))
	}
	if earlierText >= laterText {
		t.Fatalf("lexicographic order mismatch: %q should be less than %q", earlierText, laterText)
	}
	assertSQLiteTimestampText(t, "earlier", earlierText)
	assertSQLiteTimestampText(t, "later", laterText)
}

func TestSQLiteGetResourceByURL_UsesSubsecondLastSeenOrder(t *testing.T) {
	db := newSQLiteTestDB(t)

	resourceURL := "https://example.com/assets/app.css"
	olderID, err := db.CreateResource(resourceURL, strings.Repeat("c", 64), "css", "assets/test/older.css", 10)
	if err != nil {
		t.Fatalf("CreateResource older failed: %v", err)
	}
	newerID, err := db.CreateResource(resourceURL, strings.Repeat("d", 64), "css", "assets/test/newer.css", 10)
	if err != nil {
		t.Fatalf("CreateResource newer failed: %v", err)
	}

	base := time.Date(2026, 4, 24, 7, 23, 51, 0, time.UTC)
	if _, err := db.conn.Exec("UPDATE resources SET last_seen = ? WHERE id = ?", formatSQLiteTimestamp(base), olderID); err != nil {
		t.Fatalf("seed older last_seen failed: %v", err)
	}
	if _, err := db.conn.Exec("UPDATE resources SET last_seen = ? WHERE id = ?", formatSQLiteTimestamp(base.Add(100*time.Millisecond)), newerID); err != nil {
		t.Fatalf("seed newer last_seen failed: %v", err)
	}

	resource, err := db.GetResourceByURL(resourceURL)
	if err != nil {
		t.Fatalf("GetResourceByURL failed: %v", err)
	}
	if resource == nil {
		t.Fatal("expected resource, got nil")
	}
	if resource.ID != newerID {
		t.Fatalf("resource ID = %d, want %d", resource.ID, newerID)
	}
}

func TestSQLiteGetPagesByURLAndNeighbors_UseSubsecondFirstVisitedOrder(t *testing.T) {
	db := newSQLiteTestDB(t)

	testURL := "https://example.com/subsecond-history"
	olderID, err := db.CreatePage(testURL, "Older", "html/test/subsecond-older.html", strings.Repeat("e", 64), time.Now().UTC())
	if err != nil {
		t.Fatalf("CreatePage older failed: %v", err)
	}
	middleID, err := db.CreatePage(testURL, "Middle", "html/test/subsecond-middle.html", strings.Repeat("f", 64), time.Now().UTC())
	if err != nil {
		t.Fatalf("CreatePage middle failed: %v", err)
	}
	newerID, err := db.CreatePage(testURL, "Newer", "html/test/subsecond-newer.html", strings.Repeat("g", 64), time.Now().UTC())
	if err != nil {
		t.Fatalf("CreatePage newer failed: %v", err)
	}

	base := time.Date(2026, 4, 24, 7, 23, 51, 0, time.UTC)
	for _, tc := range []struct {
		id   int64
		when time.Time
	}{
		{id: olderID, when: base},
		{id: middleID, when: base.Add(100 * time.Millisecond)},
		{id: newerID, when: base.Add(200 * time.Millisecond)},
	} {
		if _, err := db.conn.Exec("UPDATE pages SET first_visited = ?, last_visited = ? WHERE id = ?", formatSQLiteTimestamp(tc.when), formatSQLiteTimestamp(tc.when), tc.id); err != nil {
			t.Fatalf("seed page %d timestamps failed: %v", tc.id, err)
		}
	}

	pages, err := db.GetPagesByURL(testURL)
	if err != nil {
		t.Fatalf("GetPagesByURL failed: %v", err)
	}
	if len(pages) != 3 {
		t.Fatalf("GetPagesByURL returned %d pages, want 3", len(pages))
	}
	if pages[0].ID != newerID || pages[1].ID != middleID || pages[2].ID != olderID {
		t.Fatalf("GetPagesByURL order = [%d, %d, %d], want [%d, %d, %d]", pages[0].ID, pages[1].ID, pages[2].ID, newerID, middleID, olderID)
	}

	prev, next, total, err := db.GetSnapshotNeighbors(testURL, middleID)
	if err != nil {
		t.Fatalf("GetSnapshotNeighbors failed: %v", err)
	}
	if total != 3 {
		t.Fatalf("total = %d, want 3", total)
	}
	if prev == nil || prev.ID != olderID {
		t.Fatalf("prev = %+v, want ID %d", prev, olderID)
	}
	if next == nil || next.ID != newerID {
		t.Fatalf("next = %+v, want ID %d", next, newerID)
	}
}

func mustParseUTC(t *testing.T, raw string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		t.Fatalf("parse %q failed: %v", raw, err)
	}
	return parsed
}

func assertSQLiteTimestampText(t *testing.T, name, value string) {
	t.Helper()

	parsed := mustParseUTC(t, value)
	if parsed.Location() != time.UTC {
		t.Fatalf("%s location = %v, want UTC", name, parsed.Location())
	}
	if want := formatSQLiteTimestamp(parsed); value != want {
		t.Fatalf("%s stored as %q, want fixed-width UTC text %q", name, value, want)
	}
}

func sqliteUserVersion(t *testing.T, db *SQLiteDB) int {
	t.Helper()

	var version int
	if err := db.conn.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("query user_version failed: %v", err)
	}
	return version
}

func sqliteTriggerExists(t *testing.T, db *SQLiteDB, name string) bool {
	t.Helper()

	var count int
	if err := db.conn.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type = 'trigger' AND name = ?", name).Scan(&count); err != nil {
		t.Fatalf("query trigger %q failed: %v", name, err)
	}
	return count > 0
}

func sqliteAuditCount(t *testing.T, db *SQLiteDB, target string) int {
	t.Helper()

	var count int
	if err := db.conn.QueryRow("SELECT count FROM migration_audit WHERE target = ?", target).Scan(&count); err != nil {
		t.Fatalf("query migration audit %q failed: %v", target, err)
	}
	return count
}
