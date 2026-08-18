package database

import (
	"fmt"
	"os"
	"testing"
	"time"
)

func TestFavoritesBasicOperations(t *testing.T) {
	// 创建临时数据库
	tmpDB := createTempDB(t)
	defer os.Remove(tmpDB)

	db, err := NewSQLite(tmpDB)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	// 创建测试页面
	pageID, err := db.CreatePage(
		"https://example.com/test",
		"Test Page",
		"html/test.html",
		"testhash",
		time.Now(),
	)
	if err != nil {
		t.Fatalf("Failed to create page: %v", err)
	}

	// 测试添加收藏
	err = db.AddFavorite(pageID)
	if err != nil {
		t.Errorf("AddFavorite failed: %v", err)
	}

	// 测试检查收藏状态
	isFav, err := db.IsFavorite(pageID)
	if err != nil {
		t.Errorf("IsFavorite failed: %v", err)
	}
	if !isFav {
		t.Error("Expected page to be favorited")
	}

	// 测试获取收藏总数
	count, err := db.GetFavoritesCount()
	if err != nil {
		t.Errorf("GetFavoritesCount failed: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected count 1, got %d", count)
	}

	// 测试列出收藏
	favorites, err := db.ListFavorites(10, 0)
	if err != nil {
		t.Errorf("ListFavorites failed: %v", err)
	}
	if len(favorites) != 1 {
		t.Errorf("Expected 1 favorite, got %d", len(favorites))
	}
	if len(favorites) > 0 && favorites[0].ID != pageID {
		t.Errorf("Expected page ID %d, got %d", pageID, favorites[0].ID)
	}

	// 测试取消收藏
	err = db.RemoveFavorite(pageID)
	if err != nil {
		t.Errorf("RemoveFavorite failed: %v", err)
	}

	// 验证已取消收藏
	isFav, err = db.IsFavorite(pageID)
	if err != nil {
		t.Errorf("IsFavorite failed: %v", err)
	}
	if isFav {
		t.Error("Expected page to not be favorited")
	}
}

func TestFavoritesIdempotency(t *testing.T) {
	tmpDB := createTempDB(t)
	defer os.Remove(tmpDB)

	db, err := NewSQLite(tmpDB)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	pageID, err := db.CreatePage(
		"https://example.com/test",
		"Test Page",
		"html/test.html",
		"testhash",
		time.Now(),
	)
	if err != nil {
		t.Fatalf("Failed to create page: %v", err)
	}

	// 添加收藏两次应该是幂等的
	err = db.AddFavorite(pageID)
	if err != nil {
		t.Errorf("First AddFavorite failed: %v", err)
	}

	err = db.AddFavorite(pageID)
	if err != nil {
		t.Errorf("Second AddFavorite should be idempotent: %v", err)
	}

	// 应该只有一个收藏
	count, err := db.GetFavoritesCount()
	if err != nil {
		t.Errorf("GetFavoritesCount failed: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected count 1 after duplicate add, got %d", count)
	}

	// 删除不存在的收藏应该是幂等的
	err = db.RemoveFavorite(pageID)
	if err != nil {
		t.Errorf("RemoveFavorite failed: %v", err)
	}

	err = db.RemoveFavorite(pageID)
	if err != nil {
		t.Errorf("Second RemoveFavorite should be idempotent: %v", err)
	}
}

func TestFavoritesForeignKeyConstraint(t *testing.T) {
	tmpDB := createTempDB(t)
	defer os.Remove(tmpDB)

	db, err := NewSQLite(tmpDB)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	// 尝试添加不存在的页面到收藏应该失败
	err = db.AddFavorite(999999)
	if err == nil {
		t.Error("Expected error when adding non-existent page to favorites")
	}
}

func TestFavoritesCascadeDelete(t *testing.T) {
	tmpDB := createTempDB(t)
	defer os.Remove(tmpDB)

	db, err := NewSQLite(tmpDB)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	pageID, err := db.CreatePage(
		"https://example.com/test",
		"Test Page",
		"html/test.html",
		"testhash",
		time.Now(),
	)
	if err != nil {
		t.Fatalf("Failed to create page: %v", err)
	}

	// 添加收藏
	err = db.AddFavorite(pageID)
	if err != nil {
		t.Fatalf("AddFavorite failed: %v", err)
	}

	// 删除页面
	err = db.DeletePage(pageID)
	if err != nil {
		t.Fatalf("DeletePage failed: %v", err)
	}

	// 收藏应该自动被删除
	count, err := db.GetFavoritesCount()
	if err != nil {
		t.Errorf("GetFavoritesCount failed: %v", err)
	}
	if count != 0 {
		t.Errorf("Expected count 0 after cascade delete, got %d", count)
	}
}

func TestFavoritesPagination(t *testing.T) {
	tmpDB := createTempDB(t)
	defer os.Remove(tmpDB)

	db, err := NewSQLite(tmpDB)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	// 创建多个页面并添加收藏
	for i := 0; i < 15; i++ {
		pageID, err := db.CreatePage(
			fmt.Sprintf("https://example.com/test%d", i),
			"Test Page",
			"html/test.html",
			"testhash",
			time.Now(),
		)
		if err != nil {
			t.Fatalf("Failed to create page: %v", err)
		}
		err = db.AddFavorite(pageID)
		if err != nil {
			t.Fatalf("AddFavorite failed: %v", err)
		}
		time.Sleep(1 * time.Millisecond) // 确保时间戳不同
	}

	// 测试第一页
	favorites, err := db.ListFavorites(10, 0)
	if err != nil {
		t.Errorf("ListFavorites page 1 failed: %v", err)
	}
	if len(favorites) != 10 {
		t.Errorf("Expected 10 favorites on page 1, got %d", len(favorites))
	}

	// 测试第二页
	favorites, err = db.ListFavorites(10, 10)
	if err != nil {
		t.Errorf("ListFavorites page 2 failed: %v", err)
	}
	if len(favorites) != 5 {
		t.Errorf("Expected 5 favorites on page 2, got %d", len(favorites))
	}
}

func TestFavoritesSearch(t *testing.T) {
	tmpDB := createTempDB(t)
	defer os.Remove(tmpDB)

	db, err := NewSQLite(tmpDB)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	// 创建测试页面
	pageID1, err := db.CreatePage(
		"https://github.com/test",
		"GitHub Test",
		"html/test1.html",
		"hash1",
		time.Now(),
	)
	if err != nil {
		t.Fatalf("Failed to create page 1: %v", err)
	}

	pageID2, err := db.CreatePage(
		"https://example.com/test",
		"Example Test",
		"html/test2.html",
		"hash2",
		time.Now(),
	)
	if err != nil {
		t.Fatalf("Failed to create page 2: %v", err)
	}

	// 添加收藏
	db.AddFavorite(pageID1)
	db.AddFavorite(pageID2)

	// 更新全文搜索索引
	db.UpdatePageBodyText(pageID1, "GitHub repository for testing")
	db.UpdatePageBodyText(pageID2, "Example website for testing")

	// 搜索 "GitHub"
	results, err := db.SearchFavorites("GitHub", 10, 0)
	if err != nil {
		t.Errorf("SearchFavorites failed: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("Expected 1 result for 'GitHub', got %d", len(results))
	}
	if len(results) > 0 && results[0].ID != pageID1 {
		t.Errorf("Expected page ID %d, got %d", pageID1, results[0].ID)
	}

	// 搜索 "testing" (两个页面都包含)
	results, err = db.SearchFavorites("testing", 10, 0)
	if err != nil {
		t.Errorf("SearchFavorites failed: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("Expected 2 results for 'testing', got %d", len(results))
	}

	// 测试搜索结果计数
	count, err := db.GetSearchFavoritesCount("testing")
	if err != nil {
		t.Errorf("GetSearchFavoritesCount failed: %v", err)
	}
	if count != 2 {
		t.Errorf("Expected count 2 for 'testing', got %d", count)
	}
}

func TestFavoritesSearchSQLInjection(t *testing.T) {
	tmpDB := createTempDB(t)
	defer os.Remove(tmpDB)

	db, err := NewSQLite(tmpDB)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	pageID, err := db.CreatePage(
		"https://example.com/test",
		"Test Page",
		"html/test.html",
		"testhash",
		time.Now(),
	)
	if err != nil {
		t.Fatalf("Failed to create page: %v", err)
	}

	if err := db.AddFavorite(pageID); err != nil {
		t.Fatalf("AddFavorite failed: %v", err)
	}
	if err := db.UpdatePageBodyText(pageID, "test content"); err != nil {
		t.Fatalf("UpdatePageBodyText failed: %v", err)
	}

	// 这些输入都不匹配 "test content"，正确参数化后应返回 0 条，
	// 且不得报错、不得删表。
	testCases := []string{
		"test'OR'1'='1",
		"test\" OR \"1\"=\"1",
		"test'); DROP TABLE favorites;--",
		"test* OR NOT",
		"test AND (1=1)",
	}

	for _, tc := range testCases {
		results, err := db.SearchFavorites(tc, 10, 0)
		if err != nil {
			t.Errorf("Query %q should not error: %v", tc, err)
			continue
		}
		if len(results) != 0 {
			t.Errorf("Query %q should match nothing, got %d items", tc, len(results))
		}
	}

	// favorites 表必须仍然存在且数据完好（DROP TABLE 注入未生效）
	count, err := db.GetFavoritesCount()
	if err != nil {
		t.Fatalf("favorites table should still be intact: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected favorites count 1 after injection attempts, got %d", count)
	}

	// 正常关键字仍能命中，确认搜索没有被破坏
	results, err := db.SearchFavorites("test content", 10, 0)
	if err != nil {
		t.Fatalf("SearchFavorites failed: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("Expected 1 result for legitimate keyword, got %d", len(results))
	}
}

// LIKE 通配符必须被转义，否则 "%" 会匹配所有收藏
func TestFavoritesSearchEscapesLikeWildcards(t *testing.T) {
	tmpDB := createTempDB(t)
	defer os.Remove(tmpDB)

	db, err := NewSQLite(tmpDB)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	pageID, err := db.CreatePage(
		"https://example.com/plain",
		"Plain Title",
		"html/plain.html",
		"hash-plain",
		time.Now(),
	)
	if err != nil {
		t.Fatalf("Failed to create page: %v", err)
	}
	if err := db.AddFavorite(pageID); err != nil {
		t.Fatalf("AddFavorite failed: %v", err)
	}

	for _, keyword := range []string{"%", "_", "%%"} {
		results, err := db.SearchFavorites(keyword, 10, 0)
		if err != nil {
			t.Errorf("SearchFavorites(%q) failed: %v", keyword, err)
			continue
		}
		if len(results) != 0 {
			t.Errorf("Wildcard %q should be treated literally, got %d results", keyword, len(results))
		}
	}
}

// 中文关键字必须能搜到
func TestFavoritesSearchChinese(t *testing.T) {
	tmpDB := createTempDB(t)
	defer os.Remove(tmpDB)

	db, err := NewSQLite(tmpDB)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	pageID, err := db.CreatePage(
		"https://example.com/cn",
		"归档工具说明",
		"html/cn.html",
		"hash-cn",
		time.Now(),
	)
	if err != nil {
		t.Fatalf("Failed to create page: %v", err)
	}
	if err := db.AddFavorite(pageID); err != nil {
		t.Fatalf("AddFavorite failed: %v", err)
	}
	if err := db.UpdatePageBodyText(pageID, "这是一个网页归档工具的测试内容"); err != nil {
		t.Fatalf("UpdatePageBodyText failed: %v", err)
	}

	for _, keyword := range []string{"归档", "工具", "测试内容"} {
		results, err := db.SearchFavorites(keyword, 10, 0)
		if err != nil {
			t.Fatalf("SearchFavorites(%q) failed: %v", keyword, err)
		}
		if len(results) != 1 {
			t.Errorf("Expected 1 result for %q, got %d", keyword, len(results))
		}

		count, err := db.GetSearchFavoritesCount(keyword)
		if err != nil {
			t.Fatalf("GetSearchFavoritesCount(%q) failed: %v", keyword, err)
		}
		if count != 1 {
			t.Errorf("Expected count 1 for %q, got %d", keyword, count)
		}
	}
}

// 搜索必须按 URL 匹配
func TestFavoritesSearchByURL(t *testing.T) {
	tmpDB := createTempDB(t)
	defer os.Remove(tmpDB)

	db, err := NewSQLite(tmpDB)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	pageID, err := db.CreatePage(
		"https://github.com/icodeface/wayback-archiver",
		"Untitled",
		"html/url.html",
		"hash-url",
		time.Now(),
	)
	if err != nil {
		t.Fatalf("Failed to create page: %v", err)
	}
	if err := db.AddFavorite(pageID); err != nil {
		t.Fatalf("AddFavorite failed: %v", err)
	}

	results, err := db.SearchFavorites("icodeface", 10, 0)
	if err != nil {
		t.Fatalf("SearchFavorites failed: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("Expected 1 result matching URL, got %d", len(results))
	}
}

// 只有被收藏的页面才应出现在收藏搜索结果里
func TestFavoritesSearchExcludesNonFavorites(t *testing.T) {
	tmpDB := createTempDB(t)
	defer os.Remove(tmpDB)

	db, err := NewSQLite(tmpDB)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	favID, err := db.CreatePage("https://example.com/a", "Shared Keyword A", "html/a.html", "hash-a", time.Now())
	if err != nil {
		t.Fatalf("Failed to create page: %v", err)
	}
	if _, err := db.CreatePage("https://example.com/b", "Shared Keyword B", "html/b.html", "hash-b", time.Now()); err != nil {
		t.Fatalf("Failed to create page: %v", err)
	}

	if err := db.AddFavorite(favID); err != nil {
		t.Fatalf("AddFavorite failed: %v", err)
	}

	results, err := db.SearchFavorites("Shared Keyword", 10, 0)
	if err != nil {
		t.Fatalf("SearchFavorites failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Expected only the favorited page, got %d", len(results))
	}
	if results[0].ID != favID {
		t.Errorf("Expected page %d, got %d", favID, results[0].ID)
	}

	count, err := db.GetSearchFavoritesCount("Shared Keyword")
	if err != nil {
		t.Fatalf("GetSearchFavoritesCount failed: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected count 1, got %d", count)
	}
}

// 分页不得因时间戳相同而重复或漏掉记录
func TestFavoritesPaginationNoOverlap(t *testing.T) {
	tmpDB := createTempDB(t)
	defer os.Remove(tmpDB)

	db, err := NewSQLite(tmpDB)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	const total = 25
	for i := 0; i < total; i++ {
		pageID, err := db.CreatePage(
			fmt.Sprintf("https://example.com/p%d", i),
			fmt.Sprintf("Page %d", i),
			fmt.Sprintf("html/p%d.html", i),
			fmt.Sprintf("hash%d", i),
			time.Now(),
		)
		if err != nil {
			t.Fatalf("Failed to create page: %v", err)
		}
		// 不 sleep，制造时间戳碰撞
		if err := db.AddFavorite(pageID); err != nil {
			t.Fatalf("AddFavorite failed: %v", err)
		}
	}

	seen := map[int64]bool{}
	for offset := 0; offset < total; offset += 10 {
		pages, err := db.ListFavorites(10, offset)
		if err != nil {
			t.Fatalf("ListFavorites(offset=%d) failed: %v", offset, err)
		}
		for _, p := range pages {
			if seen[p.ID] {
				t.Errorf("Page %d returned on more than one page of results", p.ID)
			}
			seen[p.ID] = true
		}
	}

	if len(seen) != total {
		t.Errorf("Expected %d distinct favorites across pages, got %d", total, len(seen))
	}
}

// ListFavorites 无数据时应返回空切片而非 nil（JSON 输出 [] 而不是 null）
func TestFavoritesListEmptyNotNil(t *testing.T) {
	tmpDB := createTempDB(t)
	defer os.Remove(tmpDB)

	db, err := NewSQLite(tmpDB)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	pages, err := db.ListFavorites(10, 0)
	if err != nil {
		t.Fatalf("ListFavorites failed: %v", err)
	}
	if pages == nil {
		t.Error("Expected empty slice, got nil")
	}
	if len(pages) != 0 {
		t.Errorf("Expected 0 favorites, got %d", len(pages))
	}
}

func TestFavoritesEmptySearch(t *testing.T) {
	tmpDB := createTempDB(t)
	defer os.Remove(tmpDB)

	db, err := NewSQLite(tmpDB)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	// 空搜索应该返回空结果
	results, err := db.SearchFavorites("", 10, 0)
	if err != nil {
		t.Errorf("Empty search failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("Expected 0 results for empty search, got %d", len(results))
	}

	count, err := db.GetSearchFavoritesCount("")
	if err != nil {
		t.Errorf("GetSearchFavoritesCount with empty query failed: %v", err)
	}
	if count != 0 {
		t.Errorf("Expected count 0 for empty search, got %d", count)
	}
}

// 辅助函数：创建临时数据库
func createTempDB(t *testing.T) string {
	tmpfile, err := os.CreateTemp("", "favorites_test_*.db")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpfile.Close()
	return tmpfile.Name()
}
