package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"wayback/internal/api"
	"wayback/internal/config"
	"wayback/internal/database"
	"wayback/internal/logging"
	"wayback/internal/storage"
)

// Set via -ldflags at build time
var (
	Version   = "dev"
	BuildTime = ""
)

func main() {
	// Handle --version / -v
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Printf("wayback-server %s\n", Version)
		if BuildTime != "" {
			fmt.Printf("built at %s\n", BuildTime)
		}
		return
	}
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

	// 打印启动配置摘要（不包含密码等敏感信息）
	log.Printf("Version: %s (built: %s)", Version, BuildTime)
	log.Printf("Database: %s@%s:%d/%s (sslmode=%s)",
		cfg.Database.User, cfg.Database.Host, cfg.Database.Port, cfg.Database.DBName, cfg.Database.SSLMode)
	log.Printf("Storage: data=%s, logs=%s", cfg.Storage.DataDir, cfg.Storage.LogDir)
	log.Printf("Server: %s:%d, compression_level=%d", cfg.Server.Host, cfg.Server.Port, cfg.Server.CompressionLevel)
	log.Printf("Auth: %v", cfg.Auth.Enabled())
	log.Printf("Resource: workers=%d, cache=%dMB, download_timeout=%ds, stream_threshold=%dKB",
		cfg.Resource.Workers, cfg.Resource.CacheSizeMB, cfg.Resource.DownloadTimeout, cfg.Resource.StreamThresholdKB)

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
	fileStorage := storage.NewFileStorage(cfg.Storage.DataDir, cfg.Resource.DownloadTimeout)

	// 清理上次进程崩溃/OOM kill 残留的临时文件
	if n, err := fileStorage.CleanupTmp(); err != nil {
		log.Printf("Warning: tmp cleanup failed: %v", err)
	} else if n > 0 {
		log.Printf("Cleaned up %d orphaned temp files", n)
	}

	// 初始化去重器
	dedup := storage.NewDeduplicator(db, fileStorage, cfg.Resource)

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

	// 添加请求解压缩中间件（始终启用，因为客户端可能发送压缩数据）
	r.Use(func(c *gin.Context) {
		if c.Request.Header.Get("Content-Encoding") == "gzip" {
			gzip.DefaultDecompressHandle(c)
			if c.IsAborted() {
				// 只在失败时记录日志
				log.Printf("[Decompression] Failed to decompress %s %s: %v", c.Request.Method, c.Request.URL.Path, c.Errors.Last())
				return
			}
		}
		c.Next()
	})

	// 添加响应压缩中间件（始终启用，根据 Accept-Encoding 自动协商）
	compressionLevel := cfg.Server.CompressionLevel
	if compressionLevel == -1 {
		compressionLevel = gzip.DefaultCompression
	}
	log.Printf("Response compression enabled (level: %d, auto-negotiated via Accept-Encoding)", compressionLevel)

	// 排除已压缩的文件类型，避免浪费 CPU
	r.Use(gzip.Gzip(
		compressionLevel,
		gzip.WithExcludedExtensions([]string{
			".png", ".gif", ".jpeg", ".jpg", // 图片（默认已排除，这里显式声明）
			".webp", ".svg", ".ico",         // 其他图片格式
			".mp4", ".webm", ".avi",         // 视频
			".mp3", ".ogg", ".wav",          // 音频
			".zip", ".gz", ".tar", ".rar",   // 压缩包
			".woff", ".woff2", ".ttf",       // 字体文件
		}),
	))

	api.SetupRoutes(r, handler, &cfg.Auth, Version, BuildTime)

	// 启动服务器
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	log.Printf("Server starting on %s", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
