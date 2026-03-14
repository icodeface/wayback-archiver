package main

import (
	"fmt"
	"log"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"wayback/internal/api"
	"wayback/internal/config"
	"wayback/internal/database"
	"wayback/internal/logging"
	"wayback/internal/storage"
)

func main() {
	// 加载 .env 文件（如果存在）
	// 忽略错误，因为 .env 文件是可选的（可以直接使用环境变量）
	_ = godotenv.Load()

	// 加载配置
	cfg, err := config.LoadFromEnv()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// 初始化日志系统（必须在其他组件之前）
	logger, err := logging.Setup(cfg.Storage.LogDir)
	if err != nil {
		log.Fatalf("Failed to setup logging: %v", err)
	}
	defer logger.Close()
	log.Println("Logging initialized:", cfg.Storage.LogDir)

	// 连接数据库
	db, err := database.New(
		cfg.Database.Host,
		fmt.Sprintf("%d", cfg.Database.Port),
		cfg.Database.User,
		cfg.Database.Password,
		cfg.Database.DBName,
	)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()
	log.Println("Database connected")

	// 初始化存储
	fileStorage := storage.NewFileStorage(cfg.Storage.DataDir)

	// 初始化去重器
	dedup := storage.NewDeduplicator(db, fileStorage)

	// 清理被替换的旧版本 HTML 文件（启动时执行一次）
	const htmlRetentionDays = 7 // 旧版本保留 7 天
	log.Printf("Processing HTML deletion queue (retention: %d days)...", htmlRetentionDays)
	if err := dedup.CleanupOldHTML(htmlRetentionDays); err != nil {
		log.Printf("Warning: HTML cleanup failed: %v", err)
	}

	// 启动后台 goroutine 定期清理（每天午夜执行）
	go func() {
		for {
			now := time.Now()
			// 计算到下一个午夜的时间
			next := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 1, 0, now.Location())
			time.Sleep(next.Sub(now))

			log.Printf("Running scheduled HTML deletion queue cleanup...")
			if err := dedup.CleanupOldHTML(htmlRetentionDays); err != nil {
				log.Printf("Warning: scheduled HTML cleanup failed: %v", err)
			}
		}
	}()

	// 初始化 API 处理器
	handler := api.NewHandler(dedup, db, cfg.Storage.DataDir, logger)

	// 设置 Gin
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()

	// 添加 Recovery 中间件（处理 panic）
	r.Use(gin.Recovery())

	// 添加自定义日志中间件（请求到达时立即打印）
	r.Use(func(c *gin.Context) {
		log.Printf("[HTTP] %s %s from %s", c.Request.Method, c.Request.URL.Path, c.ClientIP())
		c.Next()
	})

	api.SetupRoutes(r, handler, &cfg.Auth)

	// 启动服务器
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	log.Printf("Server starting on %s", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
