// Package mysql 是博客存储层的 MySQL 方言（github.com/go-sql-driver/mysql）。
//
// 与 SQLite 方言的差异全部收在这里：DDL 类型（VARCHAR/MEDIUMTEXT/BIGINT/AUTO_INCREMENT）、
// 列存在性探测（information_schema.columns）、自增主键回填（LastInsertId）、
// upsert 形态（ON DUPLICATE KEY UPDATE）、标签匹配表达式（CONCAT）、字符集与排序规则。
// 时间列与 SQLite 保持同一种表示（本地时间文本 VARCHAR），两库可逐字对拍。
// 迁移只做增量（CREATE TABLE IF NOT EXISTS / ADD COLUMN），绝不 DROP、TRUNCATE 或重建表。
package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"

	"blog-go/internal/store"
)

// Dialect 实现 store.Dialect。
type Dialect struct{}

// New 返回 MySQL 方言单例。
func New() *Dialect { return &Dialect{} }

// Name 返回方言名。
func (d *Dialect) Name() string { return "mysql" }

// DriverName 返回 go-sql-driver/mysql 注册的驱动名。
func (d *Dialect) DriverName() string { return "mysql" }

// BuildDSN 由分段参数拼出 go-sql-driver 的 DSN。口令只进返回值，绝不落日志。
//
// 参数取舍：utf8mb4 保中文/emoji；parseTime=false（时间列是文本，不交给驱动解析）；
// timeout 控制建连、read/writeTimeout 防止慢查询挂死。
func BuildDSN(host string, port int, name, user, password string) string {
	cfg := mysql.NewConfig()
	cfg.User = user
	cfg.Passwd = password
	cfg.Net = "tcp"
	cfg.Addr = fmt.Sprintf("%s:%d", host, port)
	cfg.DBName = name
	cfg.ParseTime = false
	cfg.Collation = "utf8mb4_unicode_ci"
	cfg.Loc = time.Local
	cfg.Timeout = 5 * time.Second
	cfg.ReadTimeout = 15 * time.Second
	cfg.WriteTimeout = 15 * time.Second
	return cfg.FormatDSN()
}

// ddl 是建表语句（幂等）。时间列与 SQLite 同构为本地时间文本 VARCHAR；
// 唯一键列用 VARCHAR(255)（utf8mb4 下标长 1020 字节，远小于 MySQL 8 的 3072 上限）。
var ddl = []string{
	`CREATE TABLE IF NOT EXISTS collections (
		id          BIGINT NOT NULL AUTO_INCREMENT,
		name        VARCHAR(255) NOT NULL,
		slug        VARCHAR(255) NOT NULL,
		public_id   VARCHAR(64) NULL,
		description TEXT NOT NULL,
		created_at  VARCHAR(32) NOT NULL DEFAULT '',
		PRIMARY KEY (id),
		UNIQUE KEY uk_collections_slug (slug),
		UNIQUE KEY uk_collections_public_id (public_id)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
	`CREATE TABLE IF NOT EXISTS users (
		id         BIGINT NOT NULL AUTO_INCREMENT,
		username   VARCHAR(255) NOT NULL,
		password   VARCHAR(255) NOT NULL,
		created_at VARCHAR(32) NOT NULL DEFAULT '',
		PRIMARY KEY (id),
		UNIQUE KEY uk_users_username (username)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
	`CREATE TABLE IF NOT EXISTS posts (
		id            BIGINT NOT NULL AUTO_INCREMENT,
		title         VARCHAR(255) NOT NULL,
		subtitle      VARCHAR(255) NOT NULL DEFAULT '',
		slug          VARCHAR(255) NOT NULL,
		public_id     VARCHAR(64) NULL,
		content       MEDIUMTEXT NOT NULL,
		excerpt       TEXT NOT NULL,
		tags          TEXT NOT NULL,
		published     TINYINT NOT NULL DEFAULT 1,
		created_at    VARCHAR(32) NOT NULL DEFAULT '',
		updated_at    VARCHAR(32) NOT NULL DEFAULT '',
		collection_id BIGINT NULL,
		PRIMARY KEY (id),
		UNIQUE KEY uk_posts_slug (slug),
		UNIQUE KEY uk_posts_public_id (public_id),
		KEY idx_posts_collection (collection_id),
		CONSTRAINT fk_posts_collection FOREIGN KEY (collection_id) REFERENCES collections (id) ON DELETE SET NULL
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
	`CREATE TABLE IF NOT EXISTS tags (
		id         BIGINT NOT NULL AUTO_INCREMENT,
		name       VARCHAR(255) NOT NULL,
		post_count BIGINT NOT NULL DEFAULT 0,
		PRIMARY KEY (id),
		UNIQUE KEY uk_tags_name (name)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
	`CREATE TABLE IF NOT EXISTS schema_version (
		version    INT NOT NULL,
		applied_at BIGINT NOT NULL,
		PRIMARY KEY (version)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
}

// migrateVersion 是记录进 schema_version 的版本号（DDL 幂等，重复执行无副作用）。
const migrateVersion = 1

// Migrate 建表 / 建索引 / 补列（幂等、事务内），并记录 schema_version。
func (d *Dialect) Migrate(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启迁移事务失败: %w", err)
	}
	for _, stmt := range ddl {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("执行建表语句失败: %w", err)
		}
	}
	for _, table := range store.MigrateTables() {
		if err := store.EnsureAlterColumns(ctx, d, tx, table); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT IGNORE INTO schema_version (version, applied_at) VALUES (?, ?)",
		migrateVersion, time.Now().UnixMilli()); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("记录 schema_version 失败: %w", err)
	}
	return tx.Commit()
}

// ColumnExists 用 information_schema.columns 探测列（限定当前库）。
func (d *Dialect) ColumnExists(ctx context.Context, q store.Querier, table, column string) (bool, error) {
	var n int
	err := q.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM information_schema.columns "+
			"WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?",
		table, column).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("读取表 %s 结构失败: %w", table, err)
	}
	return n > 0, nil
}

// InsertReturningID 用 Exec + LastInsertId 回填自增主键（MySQL 无 RETURNING）。
func (d *Dialect) InsertReturningID(ctx context.Context, q store.Querier, insertSQL string, args ...any) (int64, error) {
	res, err := q.ExecContext(ctx, insertSQL, args...)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpsertSQL 生成 ON DUPLICATE KEY UPDATE 形态（跨库搬运幂等写入）。
func (d *Dialect) UpsertSQL(table string, columns, keyColumns []string) string {
	placeholders := make([]string, len(columns))
	for i := range columns {
		placeholders[i] = "?"
	}
	assignments := make([]string, 0, len(columns))
	for _, c := range columns {
		if contains(keyColumns, c) {
			continue
		}
		assignments = append(assignments, c+" = VALUES("+c+")")
	}
	base := "INSERT INTO " + table + " (" + strings.Join(columns, ", ") + ") VALUES (" + strings.Join(placeholders, ", ") + ")"
	if len(assignments) == 0 {
		return base
	}
	return base + " ON DUPLICATE KEY UPDATE " + strings.Join(assignments, ", ")
}

// PrepareCopy 关闭本会话的外键检查。源库存在历史遗留的孤儿行（引用了已删除的父行），
// 忠实拷贝要求这些行原样落到目标库，而不是被外键约束挡下或清洗。FOREIGN_KEY_CHECKS 是会话级
// 开关，必须在与后续写事务相同的固定连接上、于事务之外设置。
func (d *Dialect) PrepareCopy(ctx context.Context, q store.Querier) error {
	if _, err := q.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS=0"); err != nil {
		return fmt.Errorf("关闭 MySQL 外键检查失败: %w", err)
	}
	return nil
}

// TagLikeClause 用 CONCAT 拼接逗号边界（MySQL 口径），含一个 `?` 占位符。
// ESCAPE '\\' 是 MySQL 字符串字面量，表示单反斜杠。
func (d *Dialect) TagLikeClause(column string) string {
	return "CONCAT(',', " + column + ", ',') LIKE ? ESCAPE '\\\\'"
}

func contains(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}
