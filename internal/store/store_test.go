package store_test

import (
	"context"
	"database/sql"
	"os"
	"strconv"
	"testing"

	"blog-go/internal/snowflake"
	"blog-go/internal/store"
)

// testDSN 构造临时 MySQL 的 DSN：优先 TEST_DB_DSN，否则由 TEST_DB_* 分段拼装。
// 未提供时跳过（无库环境下 make check 仍可全绿；有库时这些用例真跑在 MySQL 上）。
func testDSN(t *testing.T) string {
	t.Helper()
	if dsn := os.Getenv("TEST_DB_DSN"); dsn != "" {
		return dsn
	}
	name := os.Getenv("TEST_DB_NAME")
	user := os.Getenv("TEST_DB_USER")
	if name == "" || user == "" {
		t.Skip("未设置 TEST_DB_DSN（或 TEST_DB_NAME/TEST_DB_USER），跳过 MySQL 集成测试")
	}
	host := os.Getenv("TEST_DB_HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := 3306
	if raw := os.Getenv("TEST_DB_PORT"); raw != "" {
		p, err := strconv.Atoi(raw)
		if err != nil {
			t.Fatalf("TEST_DB_PORT 非法: %v", err)
		}
		port = p
	}
	return store.BuildDSN(host, port, name, user, os.Getenv("TEST_DB_PASSWORD"))
}

func openTestDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(context.Background(), testDSN(t), 4, nil)
	if err != nil {
		t.Fatalf("打开 MySQL 失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// resetSchema 清空全部表，让每个用例从确定的空库起跑。
func resetSchema(t *testing.T, db *store.DB) {
	t.Helper()
	for _, table := range []string{"posts", "collections", "tags", "users", "schema_version"} {
		if _, err := db.SQL.Exec("DROP TABLE IF EXISTS " + table); err != nil {
			t.Fatalf("清空表 %s 失败: %v", table, err)
		}
	}
}

// TestMigrateBuildsSchemaAndIsIdempotent 校验空库自建出的表 / 索引 / 列与权威 schema 完全一致，
// 且重复迁移无副作用。权威 = docs/mysql-schema-cloud.md 的云库 `blog`。
func TestMigrateBuildsSchemaAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	resetSchema(t, db)

	if err := store.Migrate(ctx, db.SQL); err != nil {
		t.Fatalf("空库建表失败: %v", err)
	}
	if err := store.Migrate(ctx, db.SQL); err != nil {
		t.Fatalf("重复迁移失败: %v", err)
	}

	wantTables := map[string]bool{
		"collections": true, "posts": true, "tags": true, "users": true, "schema_version": true,
	}
	gotTables := queryStrings(t, db.SQL,
		"SELECT table_name FROM information_schema.tables WHERE table_schema = DATABASE()")
	for name := range wantTables {
		if !gotTables[name] {
			t.Errorf("缺表 %s", name)
		}
	}
	for name := range gotTables {
		if !wantTables[name] {
			t.Errorf("多出表 %s", name)
		}
	}

	// 索引名与权威 schema 一致（注意 public_id 的唯一键名沿用 idx_ 前缀）。
	wantIndexes := []string{
		"uq_collections_slug", "idx_collections_public_id",
		"uq_posts_slug", "idx_posts_public_id",
		"uq_tags_name", "uq_users_username",
	}
	gotIndexes := queryStrings(t, db.SQL,
		"SELECT DISTINCT index_name FROM information_schema.statistics WHERE table_schema = DATABASE()")
	for _, name := range wantIndexes {
		if !gotIndexes[name] {
			t.Errorf("缺索引 %s", name)
		}
	}

	wantColumns := map[string]map[string]bool{
		"collections": {"id": true, "name": true, "slug": true, "description": true, "created_at": true, "public_id": true},
		"posts": {"id": true, "title": true, "slug": true, "content": true, "excerpt": true, "tags": true,
			"published": true, "created_at": true, "updated_at": true, "collection_id": true,
			"public_id": true, "subtitle": true},
		"tags":  {"id": true, "name": true, "post_count": true},
		"users": {"id": true, "username": true, "password": true, "created_at": true},
	}
	for table, want := range wantColumns {
		got := queryStrings(t, db.SQL,
			"SELECT column_name FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = ?", table)
		for name := range want {
			if !got[name] {
				t.Errorf("%s 缺列 %s", table, name)
			}
		}
		for name := range got {
			if !want[name] {
				t.Errorf("%s 多出列 %s", table, name)
			}
		}
	}

	// 关键列类型必须与云库一致（longtext / bigint / varchar(256)）。
	wantTypes := map[string]string{
		"collections.description": "longtext",
		"collections.slug":        "varchar(256)",
		"collections.public_id":   "varchar(256)",
		"posts.content":           "longtext",
		"posts.subtitle":          "longtext",
		"posts.published":         "bigint",
		"posts.collection_id":     "bigint",
		"posts.public_id":         "varchar(256)",
		"tags.name":               "varchar(256)",
		"users.username":          "varchar(256)",
		"users.password":          "longtext",
	}
	gotTypes := queryColumnTypes(t, db.SQL)
	for key, want := range wantTypes {
		if got := gotTypes[key]; got != want {
			t.Errorf("%s 列类型不一致: got=%q want=%q", key, got, want)
		}
	}
}

// TestOpenSelfBuildsSchemaOnEmptyDB 走服务真实的启动路径（store.Open → Migrate）：
// 清空全部表后重新 Open，应把整套表与索引自建出来，无需任何手工 DDL。
func TestOpenSelfBuildsSchemaOnEmptyDB(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	resetSchema(t, db)
	_ = db.Close()

	fresh, err := store.Open(ctx, testDSN(t), 4, nil)
	if err != nil {
		t.Fatalf("空库启动（store.Open）失败: %v", err)
	}
	t.Cleanup(func() { _ = fresh.Close() })

	got := queryStrings(t, fresh.SQL,
		"SELECT table_name FROM information_schema.tables WHERE table_schema = DATABASE()")
	for _, name := range []string{"collections", "posts", "tags", "users", "schema_version"} {
		if !got[name] {
			t.Errorf("启动后缺表 %s", name)
		}
	}
	var n int
	if err := fresh.SQL.QueryRow("SELECT COUNT(*) FROM schema_version").Scan(&n); err != nil {
		t.Fatalf("读 schema_version 失败: %v", err)
	}
	if n < 1 {
		t.Errorf("schema_version 未记录版本：count=%d", n)
	}
}

// TestMigrateAddsMissingColumnWithoutTouchingData 造一个缺 subtitle/public_id/collection_id 的老 posts 表，
// 迁移应只补列、保留历史行。
func TestMigrateAddsMissingColumnWithoutTouchingData(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	resetSchema(t, db)

	if _, err := db.SQL.Exec("CREATE TABLE posts (" +
		"id bigint NOT NULL AUTO_INCREMENT," +
		"title longtext NOT NULL," +
		"slug varchar(256) NOT NULL," +
		"content longtext NOT NULL DEFAULT (_utf8mb4'')," +
		"excerpt longtext NOT NULL DEFAULT (_utf8mb4'')," +
		"tags longtext NOT NULL DEFAULT (_utf8mb4'')," +
		"published bigint NOT NULL DEFAULT '1'," +
		"created_at longtext NOT NULL DEFAULT (now())," +
		"updated_at longtext NOT NULL DEFAULT (now())," +
		"PRIMARY KEY (id)," +
		"UNIQUE KEY uq_posts_slug (slug)" +
		") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci"); err != nil {
		t.Fatalf("建老表失败: %v", err)
	}
	if _, err := db.SQL.Exec(
		"INSERT INTO posts (title, slug, content, created_at, updated_at) VALUES (?, ?, ?, ?, ?)",
		"老文章", "legacy-post", "正文", "2026-01-02 03:04:05", "2026-01-02 03:04:05"); err != nil {
		t.Fatalf("写老表失败: %v", err)
	}

	if err := store.Migrate(ctx, db.SQL); err != nil {
		t.Fatalf("补列迁移失败: %v", err)
	}

	cols := queryStrings(t, db.SQL,
		"SELECT column_name FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'posts'")
	for _, name := range []string{"subtitle", "public_id", "collection_id"} {
		if !cols[name] {
			t.Fatalf("列 %s 未被增量补上", name)
		}
	}

	var title, createdAt string
	if err := db.SQL.QueryRow("SELECT title, created_at FROM posts WHERE slug = 'legacy-post'").Scan(&title, &createdAt); err != nil {
		t.Fatalf("读回老行失败: %v", err)
	}
	if title != "老文章" || createdAt != "2026-01-02 03:04:05" {
		t.Fatalf("老行数据未保留: title=%q created_at=%q", title, createdAt)
	}
}

// TestBackfillPublicIDs 校验启动回填：历史行按 created_at 生成非空且唯一的 public_id 字符串。
func TestBackfillPublicIDs(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	resetSchema(t, db)
	if err := store.Migrate(ctx, db.SQL); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	if _, err := db.SQL.Exec("INSERT INTO collections (name, slug, created_at) VALUES (?, ?, ?)",
		"合集", "legacy-collection", "2026-01-02 03:04:05"); err != nil {
		t.Fatalf("写合集失败: %v", err)
	}
	if _, err := db.SQL.Exec(
		"INSERT INTO posts (title, slug, created_at, updated_at) VALUES (?, ?, ?, ?)",
		"文章", "legacy-post", "2026-01-02 03:04:05", "2026-01-02 03:04:05"); err != nil {
		t.Fatalf("写文章失败: %v", err)
	}
	_ = db.Close()

	fresh, err := store.Open(ctx, testDSN(t), 4, snowflake.New(0))
	if err != nil {
		t.Fatalf("启动回填失败: %v", err)
	}
	t.Cleanup(func() { _ = fresh.Close() })

	for _, table := range []string{"posts", "collections"} {
		var n int
		if err := fresh.SQL.QueryRow("SELECT COUNT(*) FROM " + table + " WHERE public_id IS NULL OR public_id = ''").Scan(&n); err != nil {
			t.Fatalf("统计 %s 回填失败: %v", table, err)
		}
		if n != 0 {
			t.Errorf("%s 仍有 %d 行未回填 public_id", table, n)
		}
	}
}

// queryStrings 把单列查询结果收成集合。
func queryStrings(t *testing.T, db *sql.DB, query string, args ...any) map[string]bool {
	t.Helper()
	rows, err := db.Query(query, args...)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatalf("扫描失败: %v", err)
		}
		out[s] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("遍历失败: %v", err)
	}
	return out
}

// queryColumnTypes 返回 "表.列" → column_type 的映射。
func queryColumnTypes(t *testing.T, db *sql.DB) map[string]string {
	t.Helper()
	rows, err := db.Query(
		"SELECT table_name, column_name, column_type FROM information_schema.columns WHERE table_schema = DATABASE()")
	if err != nil {
		t.Fatalf("查询列类型失败: %v", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var table, column, columnType string
		if err := rows.Scan(&table, &column, &columnType); err != nil {
			t.Fatalf("扫描列类型失败: %v", err)
		}
		out[table+"."+column] = columnType
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("遍历列类型失败: %v", err)
	}
	return out
}
