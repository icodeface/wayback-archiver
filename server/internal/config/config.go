package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds all application configuration
type Config struct {
	Database DatabaseConfig
	Server   ServerConfig
	Storage  StorageConfig
	Auth     AuthConfig
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
	Port int
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
			Port: getEnvInt("SERVER_PORT", 8080),
		},
		Storage: StorageConfig{
			DataDir: getEnv("DATA_DIR", "./data"),
			LogDir:  getEnv("LOG_DIR", "./data/logs"),
		},
		Auth: AuthConfig{
			Password: getEnv("AUTH_PASSWORD", ""),
		},
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

// ConnectionString returns the PostgreSQL connection string
func (c *DatabaseConfig) ConnectionString() string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		c.Host, c.Port, c.User, c.Password, c.DBName, c.SSLMode,
	)
}
