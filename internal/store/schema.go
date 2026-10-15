package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ddl 是建表语句（幂等）。表名 / 列名 / 列类型 / 索引名逐条对齐 docs/mysql-schema-cloud.md
// 的云库 `blog`，不得另立一套；只做 CREATE TABLE IF NOT EXISTS，绝不 DROP / TRUNCATE / 重建表。
var ddl = []string{
	"CREATE TABLE IF NOT EXISTS `collections` (" +
		"`id` bigint NOT NULL AUTO_INCREMENT," +
		"`name` longtext NOT NULL," +
		"`slug` varchar(256) NOT NULL," +
		"`description` longtext NOT NULL DEFAULT (_utf8mb4'')," +
		"`created_at` longtext NOT NULL DEFAULT (now())," +
		"`public_id` varchar(256) DEFAULT NULL," +
		"PRIMARY KEY (`id`)," +
		"UNIQUE KEY `uq_collections_slug` (`slug`)," +
		"UNIQUE KEY `idx_collections_public_id` (`public_id`)" +
		") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci",
	"CREATE TABLE IF NOT EXISTS `posts` (" +
		"`id` bigint NOT NULL AUTO_INCREMENT," +
		"`title` longtext NOT NULL," +
		"`slug` varchar(256) NOT NULL," +
		"`content` longtext NOT NULL DEFAULT (_utf8mb4'')," +
		"`excerpt` longtext NOT NULL DEFAULT (_utf8mb4'')," +
		"`tags` longtext NOT NULL DEFAULT (_utf8mb4'')," +
		"`published` bigint NOT NULL DEFAULT '1'," +
		"`created_at` longtext NOT NULL DEFAULT (now())," +
		"`updated_at` longtext NOT NULL DEFAULT (now())," +
		"`collection_id` bigint DEFAULT NULL," +
		"`public_id` varchar(256) DEFAULT NULL," +
		"`subtitle` longtext NOT NULL DEFAULT (_utf8mb4'')," +
		"PRIMARY KEY (`id`)," +
		"UNIQUE KEY `uq_posts_slug` (`slug`)," +
		"UNIQUE KEY `idx_posts_public_id` (`public_id`)" +
		") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci",
	"CREATE TABLE IF NOT EXISTS `tags` (" +
		"`id` bigint NOT NULL AUTO_INCREMENT," +
		"`name` varchar(256) NOT NULL," +
		"`post_count` bigint NOT NULL DEFAULT '0'," +
		"PRIMARY KEY (`id`)," +
		"UNIQUE KEY `uq_tags_name` (`name`)" +
		") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci",
	"CREATE TABLE IF NOT EXISTS `users` (" +
		"`id` bigint NOT NULL AUTO_INCREMENT," +
		"`username` varchar(256) NOT NULL," +
		"`password` longtext NOT NULL," +
		"`created_at` longtext NOT NULL DEFAULT (now())," +
		"PRIMARY KEY (`id`)," +
		"UNIQUE KEY `uq_users_username` (`username`)" +
		") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci",
	"CREATE TABLE IF NOT EXISTS `schema_version` (" +
		"`version` int NOT NULL," +
		"`applied_at` bigint NOT NULL," +
		"PRIMARY KEY (`version`)" +
		") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci",
}

// migrateVersion 是记录进 schema_version 的版本号（DDL 幂等，重复执行无副作用）。
const migrateVersion = 1

// Migrate 建表 / 补列（幂等），并记录 schema_version。绝不 DROP / TRUNCATE / 重建表。
func Migrate(ctx context.Context, db *sql.DB) error {
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
	if err := ensureAlterColumns(ctx, tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT IGNORE INTO schema_version (version, applied_at) VALUES (?, ?)",
		migrateVersion, time.Now().UnixMilli()); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("记录 schema_version 失败: %w", err)
	}
	return tx.Commit()
}

// alterColumn 描述一个可增量补齐的列（类型对齐云库 schema）。
type alterColumn struct {
	Name string
	Type string
}

// alterSpecs 是各表用于「增量补列」的列定义，逐条对齐旧实现 db.ts 的迁移分支与云库 schema：
// posts.collection_id / posts.subtitle / posts.public_id / collections.public_id。
// 只列需要防老库缺列的列；完整建表语句见 ddl。
var alterSpecs = map[string][]alterColumn{
	"collections": {
		{Name: "public_id", Type: "varchar(256) DEFAULT NULL"},
	},
	"posts": {
		{Name: "collection_id", Type: "bigint DEFAULT NULL"},
		{Name: "subtitle", Type: "longtext NOT NULL DEFAULT (_utf8mb4'')"},
		{Name: "public_id", Type: "varchar(256) DEFAULT NULL"},
	},
}

// ensureOrder 是执行「增量补列」的表顺序（父表在前，确定性、便于复现）。
var ensureOrder = []string{"collections", "posts"}

// ensureAlterColumns 逐表补齐缺失列（幂等）；表名 / 列名为受控常量，无注入面。
func ensureAlterColumns(ctx context.Context, q Querier) error {
	for _, table := range ensureOrder {
		for _, spec := range alterSpecs[table] {
			exists, err := columnExists(ctx, q, table, spec.Name)
			if err != nil {
				return err
			}
			if exists {
				continue
			}
			if _, err := q.ExecContext(ctx,
				"ALTER TABLE "+table+" ADD COLUMN "+spec.Name+" "+spec.Type); err != nil {
				return err
			}
		}
	}
	return nil
}

// columnExists 用 information_schema.columns 探测列（限定当前库）。
func columnExists(ctx context.Context, q Querier, table, column string) (bool, error) {
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
