package config

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
)

// Config holds all application configuration
type Config struct {
	Database DatabaseConfig
	Server   ServerConfig
	Storage  StorageConfig
	Auth     AuthConfig
	Resource ResourceConfig
}

// DatabaseConfig holds database connection settings
type DatabaseConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	DBName   string
	SSLMode  string
}

// ServerConfig holds HTTP server settings
type ServerConfig struct {
	Host             string
	Port             int
	CompressionLevel int // Compression level (1-9, -1=default). Response compression always enabled, auto-negotiated via Accept-Encoding
}

// StorageConfig holds storage settings
type StorageConfig struct {
	DataDir string
	LogDir  string
}

// AuthConfig holds authentication settings
type AuthConfig struct {
	Password string
}

const AuthUsername = "wayback"

// ResourceConfig holds resource processing settings
type ResourceConfig struct {
	Workers           int // 并发下载 worker 数量（默认 = CPU 核心数 × 4，最少 2）
	CacheSizeMB       int // 资源缓存池大小（MB，默认 = 可用内存的 10%）
	DownloadTimeout   int // 单个资源下载超时（秒，默认 30）
	StreamThresholdKB int // 大文件流式落盘阈值（KB，默认 2048 即 2MB）
}

// Enabled returns true if authentication is enabled
func (a *AuthConfig) Enabled() bool {
	return a.Password != ""
}

// LoadFromEnv loads configuration from environment variables with sensible defaults
func LoadFromEnv() (*Config, error) {
	// 默认使用当前系统用户名作为数据库用户（PostgreSQL 默认行为）
	defaultUser := os.Getenv("USER")
	if defaultUser == "" {
		defaultUser = "postgres" // fallback for systems without USER env var
	}

	cfg := &Config{
		Database: DatabaseConfig{
			Host:     getEnv("DB_HOST", "localhost"),
			Port:     getEnvInt("DB_PORT", 5432),
			User:     getEnv("DB_USER", defaultUser),
			Password: getEnv("DB_PASSWORD", ""),
			DBName:   getEnv("DB_NAME", "wayback"),
			SSLMode:  getEnv("DB_SSLMODE", "disable"),
		},
		Server: ServerConfig{
			Host:             getEnv("SERVER_HOST", "127.0.0.1"),
			Port:             getEnvInt("SERVER_PORT", 8080),
			CompressionLevel: getEnvInt("COMPRESSION_LEVEL", -1), // -1 = DefaultCompression
		},
		Storage: StorageConfig{
			DataDir: getEnv("DATA_DIR", "./data"),
			LogDir:  getEnv("LOG_DIR", "./data/logs"),
		},
		Auth: AuthConfig{
			Password: getEnv("AUTH_PASSWORD", ""),
		},
		Resource: detectResourceConfig(),
	}

	// Validate required fields
	if cfg.Database.User == "" {
		return nil, fmt.Errorf("DB_USER is required")
	}
	if cfg.Database.DBName == "" {
		return nil, fmt.Errorf("DB_NAME is required")
	}

	return cfg, nil
}

// getEnv retrieves an environment variable or returns a default value
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvInt retrieves an integer environment variable or returns a default value
func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

// getEnvBool retrieves a boolean environment variable or returns a default value
func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		}
	}
	return defaultValue
}

// ConnectionString returns the PostgreSQL connection string
func (c *DatabaseConfig) ConnectionString() string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		c.Host, c.Port, c.User, c.Password, c.DBName, c.SSLMode,
	)
}

// detectResourceConfig 根据系统资源自动检测合理的资源处理配置，支持环境变量覆盖
func detectResourceConfig() ResourceConfig {
	// 1. 并发 worker 数量：默认 = CPU 核心数 × 4，最少 2
	// 资源下载是 I/O 密集型任务（网络等待），不受 CPU 核心数限制
	defaultWorkers := runtime.NumCPU() * 4
	if defaultWorkers < 2 {
		defaultWorkers = 2
	}

	// 2. 缓存池大小：默认 = 可用内存的 10%
	totalMemMB := getTotalMemoryMB()
	defaultCacheMB := totalMemMB / 10

	// 3. 从环境变量覆盖（0 表示使用自动检测的默认值）
	workers := defaultWorkers
	if v := getEnvInt("RESOURCE_WORKERS", 0); v > 0 {
		workers = v
	}
	cacheSizeMB := defaultCacheMB
	if v := getEnvInt("RESOURCE_CACHE_MB", 0); v > 0 {
		cacheSizeMB = v
	}
	downloadTimeout := getEnvInt("RESOURCE_DOWNLOAD_TIMEOUT", 30)
	streamThresholdKB := getEnvInt("RESOURCE_STREAM_THRESHOLD_KB", 2048)

	// 安全边界
	if workers < 1 {
		workers = 1
	}
	if cacheSizeMB < 1 {
		cacheSizeMB = 1
	}
	if cacheSizeMB > totalMemMB {
		cacheSizeMB = totalMemMB
	}
	if downloadTimeout < 5 {
		downloadTimeout = 5
	}
	if streamThresholdKB < 0 {
		streamThresholdKB = 0
	}

	cfg := ResourceConfig{
		Workers:           workers,
		CacheSizeMB:       cacheSizeMB,
		DownloadTimeout:   downloadTimeout,
		StreamThresholdKB: streamThresholdKB,
	}

	return cfg
}
