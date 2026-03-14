package main

import (
	"fmt"
	"log"

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

	// 初始化 API 处理器
	handler := api.NewHandler(dedup, db, cfg.Storage.DataDir, logger)

	// 设置 Gin
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()
	api.SetupRoutes(r, handler, &cfg.Auth)

	// 启动服务器
	log.Printf("Server starting on port %d", cfg.Server.Port)
	if err := r.Run(fmt.Sprintf(":%d", cfg.Server.Port)); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
