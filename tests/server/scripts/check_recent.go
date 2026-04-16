package main

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/lib/pq"
)

func main() {
	connStr := "host=localhost port=5432 user=postgres dbname=wayback sslmode=disable"
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// 查询最近的页面记录
	rows, err := db.Query(`
		SELECT id, url, title, captured_at, last_visited
		FROM pages
		ORDER BY COALESCE(last_visited, captured_at) DESC
		LIMIT 5
	`)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	fmt.Println("=== Recent Pages ===")
	for rows.Next() {
		var id int64
		var url, title string
		var capturedAt time.Time
		var lastVisited sql.NullTime
		if err := rows.Scan(&id, &url, &title, &capturedAt, &lastVisited); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("\nID: %d\n", id)
		fmt.Printf("URL: %s\n", url)
		fmt.Printf("Title: %s\n", title)
		fmt.Printf("Captured: %s\n", capturedAt.Format("2006-01-02 15:04:05"))
		if lastVisited.Valid {
			fmt.Printf("Last visited: %s\n", lastVisited.Time.Format("2006-01-02 15:04:05"))
		}
	}
}
