package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"

	_ "modernc.org/sqlite"
)

// testDBSeq 给同一用例内多次建库命名兜底，避免纳秒时间戳撞车。
var testDBSeq atomic.Int64

// TestMigrateContinuesAutoIncrement 是本工具最关键的回归：把带 sqlite_sequence（含已删除行、
// seq > max(id)）的旧库搬进 MySQL 后，目标 AUTO_INCREMENT 必须延续到旧实现的下一个 id
// （seq+1），而不是回退到 max(id)+1；随后插入的新行拿到的 id 必须等于 seq+1。
//
// 另含两条不变量：目标已存在更大的 AUTO_INCREMENT 时只升不降（不覆盖）；--verify 能识别该关系。
//
// 需要一个临时 MySQL（只在验收时用，不碰生产库）：提供
// MIGRATE_TEST_MYSQL_HOST / MIGRATE_TEST_MYSQL_USER，可选 MIGRATE_TEST_MYSQL_PORT（默认 3306）
// 与 MIGRATE_TEST_MYSQL_PASSWORD；未提供时跳过。用例自建、自删临时库。
func TestMigrateContinuesAutoIncrement(t *testing.T) {
	host := os.Getenv("MIGRATE_TEST_MYSQL_HOST")
	user := os.Getenv("MIGRATE_TEST_MYSQL_USER")
	if host == "" || user == "" {
		t.Skip("未设置 MIGRATE_TEST_MYSQL_HOST/USER，跳过 MySQL 集成用例")
	}
	port := 3306
	if v := os.Getenv("MIGRATE_TEST_MYSQL_PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			t.Fatalf("MIGRATE_TEST_MYSQL_PORT 非法: %v", err)
		}
		port = p
	}
	pass := os.Getenv("MIGRATE_TEST_MYSQL_PASSWORD")

	// 造源库：posts 插 5 行再删 id=4、5，使 sqlite_sequence.seq=5 > max(id)=3；
	// 旧实现下一条新文章应为 6，而 max(id)+1 只会得到 4。
	srcPath := filepath.Join(t.TempDir(), "blog.db")
	if err := buildFixtureSQLite(context.Background(), srcPath); err != nil {
		t.Fatalf("造临时 SQLite 失败: %v", err)
	}

	// 场景一：延续 + 校验 + 新插入的 id。
	continuityDB, continuityDSN := testCreateDatabase(t, host, port, user, pass)
	if err := run([]string{"--sqlite", srcPath, "--mysql-dsn", continuityDSN}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	if got := testReadAutoIncrement(t, host, port, user, pass, continuityDB, "posts"); got != 6 {
		t.Fatalf("迁移后 posts AUTO_INCREMENT = %d，期望 6（源 seq 5 + 1，与旧实现的下一条一致）", got)
	}
	if err := run([]string{"--sqlite", srcPath, "--mysql-dsn", continuityDSN, "--verify"}); err != nil {
		t.Fatalf("verify 应在自增序号延续后通过: %v", err)
	}
	if got := testInsertPost(t, continuityDSN); got != 6 {
		t.Fatalf("迁移后插入新文章得到 id = %d，期望 6（与旧实现一致，不复用已删除的 id）", got)
	}

	// 场景二：目标已有更大的 AUTO_INCREMENT 时必须只升不降。
	onlyRaiseDB, onlyRaiseDSN := testCreateDatabase(t, host, port, user, pass)
	if err := run([]string{"--sqlite", srcPath, "--mysql-dsn", onlyRaiseDSN}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	conn, err := sql.Open("mysql", onlyRaiseDSN)
	if err != nil {
		t.Fatalf("连接目标库失败: %v", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(context.Background(), "ALTER TABLE posts AUTO_INCREMENT = 100"); err != nil {
		t.Fatalf("预置更大 AUTO_INCREMENT 失败: %v", err)
	}
	if err := run([]string{"--sqlite", srcPath, "--mysql-dsn", onlyRaiseDSN}); err != nil {
		t.Fatalf("重复迁移失败: %v", err)
	}
	if got := testReadAutoIncrement(t, host, port, user, pass, onlyRaiseDB, "posts"); got != 100 {
		t.Fatalf("重复迁移后 posts AUTO_INCREMENT = %d，期望保留 100（只升不降）", got)
	}
}

// testCreateDatabase 在临时 MySQL 上建一个唯一命名的空库，注册清理后返回库名与 DSN。
func testCreateDatabase(t *testing.T, host string, port int, user, pass string) (string, string) {
	t.Helper()
	ctx := context.Background()
	name := fmt.Sprintf("migrate_test_%d_%d", time.Now().UnixNano(), testDBSeq.Add(1))
	admin, err := sql.Open("mysql", testDSN(host, port, user, pass, ""))
	if err != nil {
		t.Fatalf("连接临时 MySQL 失败: %v", err)
	}
	if _, err := admin.ExecContext(ctx,
		"CREATE DATABASE `"+name+"` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci"); err != nil {
		_ = admin.Close()
		t.Fatalf("创建临时库 %s 失败: %v", name, err)
	}
	t.Cleanup(func() {
		_, _ = admin.ExecContext(context.Background(), "DROP DATABASE IF EXISTS `"+name+"`")
		_ = admin.Close()
	})
	return name, testDSN(host, port, user, pass, name)
}

// buildFixtureSQLite 建一张列结构与工具 columns 一致的旧库，并制造 seq > max(id) 的缺口。
func buildFixtureSQLite(ctx context.Context, path string) error {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return err
	}
	defer db.Close()
	ddl := []string{
		`CREATE TABLE collections (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, ` +
			`slug TEXT NOT NULL UNIQUE, description TEXT NOT NULL DEFAULT '', ` +
			`created_at TEXT NOT NULL DEFAULT (datetime('now','localtime')), public_id TEXT)`,
		`CREATE TABLE posts (id INTEGER PRIMARY KEY AUTOINCREMENT, title TEXT NOT NULL, ` +
			`slug TEXT NOT NULL UNIQUE, content TEXT NOT NULL DEFAULT '', excerpt TEXT NOT NULL DEFAULT '', ` +
			`tags TEXT NOT NULL DEFAULT '', published INTEGER NOT NULL DEFAULT 1, ` +
			`created_at TEXT NOT NULL DEFAULT (datetime('now','localtime')), ` +
			`updated_at TEXT NOT NULL DEFAULT (datetime('now','localtime')), collection_id INTEGER, ` +
			`public_id TEXT, subtitle TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE users (id INTEGER PRIMARY KEY AUTOINCREMENT, username TEXT NOT NULL UNIQUE, ` +
			`password TEXT NOT NULL, created_at TEXT NOT NULL DEFAULT (datetime('now','localtime')))`,
		`CREATE TABLE tags (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL UNIQUE, ` +
			`post_count INTEGER NOT NULL DEFAULT 0)`,
	}
	for _, stmt := range ddl {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	const t0 = "2026-01-01 00:00:00"
	for i, slug := range []string{"p1", "p2", "p3", "p4", "p5"} {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO posts (title, slug, content, excerpt, tags, published, created_at, updated_at, subtitle)
			 VALUES (?, ?, '', '', 'tag', 1, ?, ?, '')`,
			fmt.Sprintf("文章 %d", i+1), slug, t0, t0); err != nil {
			return err
		}
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM posts WHERE id IN (4, 5)`); err != nil {
		return err
	}
	for i, slug := range []string{"c1", "c2"} {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO collections (name, slug, description, created_at) VALUES (?, ?, '', ?)`,
			fmt.Sprintf("合集 %d", i+1), slug, t0); err != nil {
			return err
		}
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO users (username, password, created_at) VALUES ('admin', 'x', ?)`, t0); err != nil {
		return err
	}
	for _, name := range []string{"tag-a", "tag-b"} {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO tags (name, post_count) VALUES (?, 1)`, name); err != nil {
			return err
		}
	}
	return nil
}

// testInsertPost 往目标 posts 插一行，返回 MySQL 分配的自增 id。
func testInsertPost(t *testing.T, dsn string) int64 {
	t.Helper()
	conn, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("连接目标库失败: %v", err)
	}
	defer conn.Close()
	res, err := conn.Exec(
		`INSERT INTO posts (title, slug, content, excerpt, tags, published, created_at, updated_at, subtitle)
		 VALUES ('迁移后新文章', 'after-migrate', '', '', '', 1, '2026-01-01 00:00:00', '2026-01-01 00:00:00', '')`)
	if err != nil {
		t.Fatalf("插入新文章失败: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("读取 LastInsertId 失败: %v", err)
	}
	return id
}

// testReadAutoIncrement 在固定连接上关掉 information_schema 统计缓存后读目标表 AUTO_INCREMENT。
func testReadAutoIncrement(t *testing.T, host string, port int, user, pass, dbName, table string) int64 {
	t.Helper()
	ctx := context.Background()
	db, err := sql.Open("mysql", testDSN(host, port, user, pass, dbName))
	if err != nil {
		t.Fatalf("连接目标库失败: %v", err)
	}
	defer db.Close()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("获取目标库连接失败: %v", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "SET SESSION information_schema_stats_expiry = 0"); err != nil {
		t.Fatalf("关闭 information_schema 统计缓存失败: %v", err)
	}
	var n sql.NullInt64
	if err := conn.QueryRowContext(ctx,
		"SELECT AUTO_INCREMENT FROM information_schema.TABLES WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?",
		table).Scan(&n); err != nil {
		t.Fatalf("读取 AUTO_INCREMENT 失败: %v", err)
	}
	if !n.Valid {
		t.Fatalf("表 %s 无 AUTO_INCREMENT", table)
	}
	return n.Int64
}

// testDSN 组装测试用 DSN（dbName 可为空以连接服务器本身）。
func testDSN(host string, port int, user, pass, dbName string) string {
	cfg := mysql.NewConfig()
	cfg.User = user
	cfg.Passwd = pass
	cfg.Net = "tcp"
	cfg.Addr = fmt.Sprintf("%s:%d", host, port)
	cfg.DBName = dbName
	cfg.ParseTime = false
	cfg.Collation = "utf8mb4_0900_ai_ci"
	cfg.Timeout = 5 * time.Second
	return cfg.FormatDSN()
}
