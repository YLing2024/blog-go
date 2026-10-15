// Package store 是博客数据（MySQL）的统一访问层。
//
// 2026-10-16 口径变更后本服务只支持 MySQL：驱动固定为 github.com/go-sql-driver/mysql，
// 不再有 SQLite 依赖、DB_DRIVER 开关或方言分支。建表与增量迁移一并收在本包（见 schema.go）。
// 所有查询一律使用 `?` 占位符，不做字符串拼接。
package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	// 注册 database/sql 驱动名 "mysql"。
	_ "github.com/go-sql-driver/mysql"

	"blog-go/internal/snowflake"
)

// Querier 是 *sql.DB 与 *sql.Tx 的公共子集，便于把读写放进事务。
type Querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// DB 是 MySQL 连接池句柄。
type DB struct {
	SQL *sql.DB
}

// Open 打开 MySQL 连接池、跑一次幂等迁移，并为历史数据回填 public_id（雪花 ID）。
func Open(ctx context.Context, dsn string, maxOpenConns int, gen *snowflake.Generator) (*DB, error) {
	sqlDB, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}
	if maxOpenConns > 0 {
		sqlDB.SetMaxOpenConns(maxOpenConns)
		sqlDB.SetMaxIdleConns(maxOpenConns)
	}
	sqlDB.SetConnMaxLifetime(0)
	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("连接数据库失败: %w", err)
	}
	if err := Migrate(ctx, sqlDB); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	db := &DB{SQL: sqlDB}
	if gen != nil {
		if err := backfillPublicIDs(ctx, sqlDB, gen); err != nil {
			_ = sqlDB.Close()
			return nil, err
		}
	}
	return db, nil
}

// Close 关闭底层连接池。
func (db *DB) Close() error { return db.SQL.Close() }

// Tx 在单个事务里执行 fn；fn 返回错误则回滚。
func (db *DB) Tx(ctx context.Context, fn func(q Querier) error) error {
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// InsertReturningID 执行单行 INSERT 并回填自增主键（MySQL 无 RETURNING，用 LastInsertId）。
func InsertReturningID(ctx context.Context, q Querier, insertSQL string, args ...any) (int64, error) {
	res, err := q.ExecContext(ctx, insertSQL, args...)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// TagLikeClause 返回「标签列按逗号边界精确匹配」的 SQL 片段，含一个 `?` 占位符。
// ESCAPE '\\' 是 MySQL 字符串字面量，表示单反斜杠。
func TagLikeClause(column string) string {
	return "CONCAT(',', " + column + ", ',') LIKE ? ESCAPE '\\\\'"
}

// NowText 返回旧实现风格的时间文本（本地时区 `YYYY-MM-DD HH:MM:SS`），
// 与旧 SQLite 写入的时间表示逐字一致（MySQL 侧是 longtext 列，不交给驱动解析）。
func NowText() string { return time.Now().Format("2006-01-02 15:04:05") }
