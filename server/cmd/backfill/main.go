package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"wayback/internal/config"
	"wayback/internal/database"
	"wayback/internal/storage"
)

func main() {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	db, err := database.New(
		cfg.Database.Host,
		fmt.Sprintf("%d", cfg.Database.Port),
		cfg.Database.User,
		cfg.Database.Password,
		cfg.Database.DBName,
		cfg.Database.SSLMode,
	)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	pages, err := db.GetPagesWithoutBodyText()
	if err != nil {
		log.Fatalf("Failed to get pages: %v", err)
	}

	log.Printf("Found %d pages without body_text", len(pages))

	success := 0
	for _, page := range pages {
		htmlPath := filepath.Join(cfg.Storage.DataDir, page.HTMLPath)
		htmlContent, err := os.ReadFile(htmlPath)
		if err != nil {
			log.Printf("Skip page %d (%s): %v", page.ID, page.URL, err)
			continue
		}

		bodyText := storage.ExtractBodyText(string(htmlContent))
		if bodyText == "" {
			continue
		}

		if err := db.UpdatePageBodyText(page.ID, bodyText); err != nil {
			log.Printf("Failed to update page %d: %v", page.ID, err)
			continue
		}

		success++
		log.Printf("Updated page %d: %s (text: %d chars)", page.ID, page.URL, len(bodyText))
	}

	log.Printf("Done. Updated %d/%d pages", success, len(pages))
}
