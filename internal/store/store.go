// Package store 是博客数据（SQLite / MySQL）的统一访问层。
//
// 业务 SQL 收在各领域包（posts / collections / tags / auth），两库差异（DDL、列存在性探测、
// 自增主键回填、upsert 形态、标签匹配表达式）全部下沉到 Dialect 接口的两套实现，
// 业务层不感知驱动。所有查询用 `?` 占位符（modernc sqlite 与 go-sql-driver/mysql 均支持）。
package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"blog-go/internal/snowflake"
)

// Querier 是 *sql.DB 与 *sql.Tx 的公共子集，便于把读写放进事务。
type Querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Dialect 抽象两库差异。实现见 internal/sqlite 与 internal/mysql。
type Dialect interface {
	// Name 返回 "sqlite" / "mysql"。
	Name() string
	// DriverName 返回 database/sql 驱动名。
	DriverName() string
	// Migrate 以增量方式建表 / 建索引 / 补列（幂等、事务内，禁止 DROP/TRUNCATE/重建表）。
	Migrate(ctx context.Context, db *sql.DB) error
	// ColumnExists 探测列是否存在。
	ColumnExists(ctx context.Context, q Querier, table, column string) (bool, error)
	// InsertReturningID 执行单行 INSERT 并回填自增主键。
	InsertReturningID(ctx context.Context, q Querier, insertSQL string, args ...any) (int64, error)
	// UpsertSQL 返回按主键冲突即更新的 INSERT 语句（供跨库搬运幂等写入）。
	UpsertSQL(table string, columns, keyColumns []string) string
	// PrepareCopy 在跨库搬运前把连接置为「忠实拷贝」模式（关外键强制）。
	PrepareCopy(ctx context.Context, q Querier) error
	// TagLikeClause 返回「标签列按逗号边界精确匹配」的 SQL 片段，含一个 `?` 占位符。
	// sqlite 用 `||` 拼接，mysql 用 CONCAT()。
	TagLikeClause(column string) string
}

// DB 是数据库句柄 + 方言。
type DB struct {
	SQL     *sql.DB
	dialect Dialect
}

// Dialect 返回当前方言（名称 / 能力）。
func (db *DB) Dialect() Dialect { return db.dialect }

// Driver 返回驱动名（sqlite / mysql）。
func (db *DB) Driver() string { return db.dialect.Name() }

// Open 打开数据库、跑增量迁移，并为历史数据回填 public_id（雪花 ID）。
func Open(ctx context.Context, dialect Dialect, dsn string, maxOpenConns int, gen *snowflake.Generator) (*DB, error) {
	sqlDB, err := sql.Open(dialect.DriverName(), dsn)
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
	if err := dialect.Migrate(ctx, sqlDB); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	db := &DB{SQL: sqlDB, dialect: dialect}
	if gen != nil {
		if err := backfillPublicIDs(ctx, sqlDB, gen); err != nil {
			_ = sqlDB.Close()
			return nil, err
		}
	}
	return db, nil
}

// OpenRaw 打开数据库但不执行迁移：跨库搬运时用它打开源库，保证源库只被 SELECT。
func OpenRaw(ctx context.Context, dialect Dialect, dsn string, maxOpenConns int) (*DB, error) {
	sqlDB, err := sql.Open(dialect.DriverName(), dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}
	if maxOpenConns > 0 {
		sqlDB.SetMaxOpenConns(maxOpenConns)
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("连接数据库失败: %w", err)
	}
	return &DB{SQL: sqlDB, dialect: dialect}, nil
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

// NowText 返回旧实现风格的时间文本（本地时区 `YYYY-MM-DD HH:MM:SS`），
// 让两库写入的时间表示与旧 SQLite 逐字一致。
func NowText() string { return time.Now().Format("2006-01-02 15:04:05") }
