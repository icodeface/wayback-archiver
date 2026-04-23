package database

import (
	"fmt"

	"wayback/internal/config"
)

// Open 根据配置创建数据库实例
func Open(cfg *config.DatabaseConfig) (Database, error) {
	switch cfg.Type {
	case "sqlite":
		return NewSQLite(cfg.Path)
	case "postgres", "postgresql":
		sslmode := cfg.SSLMode
		if sslmode == "" {
			sslmode = "disable"
		}
		return NewPostgres(
			cfg.Host,
			fmt.Sprintf("%d", cfg.Port),
			cfg.User,
			cfg.Password,
			cfg.DBName,
			sslmode,
		)
	default:
		return nil, fmt.Errorf("unsupported database type: %q (use \"sqlite\" or \"postgres\")", cfg.Type)
	}
}
