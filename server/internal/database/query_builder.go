package database

import (
	"fmt"
	"strings"

	"github.com/lib/pq"
)

// DBType 数据库类型
type DBType string

const (
	DBTypePostgreSQL DBType = "postgres"
	DBTypeSQLite     DBType = "sqlite"
)

// QueryBuilder 处理不同数据库的 SQL 语法差异
type QueryBuilder struct {
	dbType DBType
}

// NewQueryBuilder 创建查询构建器
func NewQueryBuilder(dbType DBType) *QueryBuilder {
	return &QueryBuilder{dbType: dbType}
}

// Placeholder 返回占位符（PostgreSQL: $1, SQLite: ?）
func (qb *QueryBuilder) Placeholder(index int) string {
	if qb.dbType == DBTypePostgreSQL {
		return fmt.Sprintf("$%d", index)
	}
	return "?"
}

// CaseInsensitiveLike 返回大小写不敏感的 LIKE（PostgreSQL: ILIKE, SQLite: LIKE）
// SQLite 的 LIKE 默认大小写不敏感
func (qb *QueryBuilder) CaseInsensitiveLike() string {
	if qb.dbType == DBTypePostgreSQL {
		return "ILIKE"
	}
	return "LIKE"
}

// CurrentTimestamp 返回当前时间戳（PostgreSQL: NOW(), SQLite: CURRENT_TIMESTAMP）
func (qb *QueryBuilder) CurrentTimestamp() string {
	if qb.dbType == DBTypePostgreSQL {
		return "NOW()"
	}
	return "CURRENT_TIMESTAMP"
}

// BuildArrayContains 构建数组包含查询
// PostgreSQL: id = ANY($1) + pq.Array(values)
// SQLite: id IN (?, ?, ...)
func (qb *QueryBuilder) BuildArrayContains(column string, values []int64, startIndex int) (string, []interface{}) {
	if qb.dbType == DBTypePostgreSQL {
		return fmt.Sprintf("%s = ANY($%d)", column, startIndex), []interface{}{pq.Array(values)}
	}

	// SQLite: 展开为 IN (?, ?, ...)
	placeholders := make([]string, len(values))
	args := make([]interface{}, len(values))
	for i, v := range values {
		placeholders[i] = "?"
		args[i] = v
	}
	return fmt.Sprintf("%s IN (%s)", column, strings.Join(placeholders, ", ")), args
}

// ExtractDomain 返回从 URL 提取域名的 SQL 表达式
// PostgreSQL: substring(url from '://([^/]+)')
// SQLite: 不支持 SQL 层面提取，应在 Go 代码中使用 extractDomain() 函数
func (qb *QueryBuilder) ExtractDomain(urlColumn string) string {
	if qb.dbType == DBTypePostgreSQL {
		return fmt.Sprintf("substring(%s from '://([^/]+)')", urlColumn)
	}
	// SQLite: 返回空字符串，调用方应在 Go 层面处理
	// 参考 sqlite.go 的 ensureDomainColumn() 实现
	return "''"
}
