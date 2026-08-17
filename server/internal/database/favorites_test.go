package database

import (
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
			"https://example.com/test"+string(rune(i)),
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

	db.AddFavorite(pageID)
	db.UpdatePageBodyText(pageID, "test content")

	// 测试 SQL 注入攻击字符串
	testCases := []string{
		"test'OR'1'='1",
		"test\" OR \"1\"=\"1",
		"test'); DROP TABLE favorites;--",
		"test* OR NOT",
		"test AND (1=1)",
	}

	for _, tc := range testCases {
		// 这些查询不应该导致错误或返回异常结果
		results, err := db.SearchFavorites(tc, 10, 0)
		if err != nil {
			t.Logf("Query '%s' returned error (expected, sanitization working): %v", tc, err)
		}
		// 结果应该是空或正常的搜索结果
		if len(results) > 1 {
			t.Errorf("Query '%s' returned unexpected results: %d items", tc, len(results))
		}
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
