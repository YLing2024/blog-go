package store

import "context"

// ColumnSpec 描述一个可增量补齐的列：两库类型显式给出（不把业务 SQL 降级成公共子集）。
type ColumnSpec struct {
	Name       string
	SQLiteType string
	MySQLType  string
}

// alterSpecs 是各表用于「增量补列」的列定义，逐条对齐旧实现 db.ts 的迁移分支：
// posts.collection_id / posts.subtitle / posts.public_id / collections.public_id。
// 只列需要防老库缺列的列；完整建表语句见两套方言的 Migrate。
var alterSpecs = map[string][]ColumnSpec{
	"collections": {
		{Name: "public_id", SQLiteType: "TEXT", MySQLType: "VARCHAR(64) NULL"},
	},
	"posts": {
		{Name: "collection_id", SQLiteType: "INTEGER REFERENCES collections(id) ON DELETE SET NULL", MySQLType: "BIGINT NULL"},
		{Name: "subtitle", SQLiteType: "TEXT NOT NULL DEFAULT ''", MySQLType: "VARCHAR(255) NOT NULL DEFAULT ''"},
		{Name: "public_id", SQLiteType: "TEXT", MySQLType: "VARCHAR(64) NULL"},
	},
}

// ensureOrder 是执行「增量补列」的表顺序（父表在前，确定性、便于复现）。
var ensureOrder = []string{"collections", "posts"}

// MigrateTables 返回需要执行增量补列的表（顺序确定）。
func MigrateTables() []string { return ensureOrder }

// EnsureAlterColumns 逐表补齐缺失列（幂等）。name 为方言名。
func EnsureAlterColumns(ctx context.Context, d Dialect, q Querier, table string) error {
	for _, spec := range alterSpecs[table] {
		exists, err := d.ColumnExists(ctx, q, table, spec.Name)
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		typ := spec.MySQLType
		if d.Name() == "sqlite" {
			typ = spec.SQLiteType
		}
		if _, err := q.ExecContext(ctx, "ALTER TABLE "+table+" ADD COLUMN "+spec.Name+" "+typ); err != nil {
			return err
		}
	}
	return nil
}

// CopyTables 是跨库搬运的表顺序（先父后子，满足外键）。
var CopyTables = []string{"collections", "posts", "users", "tags"}

// copyColumns 是各表的显式列顺序（与建表列一致，供逐行拷贝）。
var copyColumns = map[string][]string{
	"collections": {"id", "name", "slug", "public_id", "description", "created_at"},
	"posts":       {"id", "title", "subtitle", "slug", "public_id", "content", "excerpt", "tags", "published", "created_at", "updated_at", "collection_id"},
	"users":       {"id", "username", "password", "created_at"},
	"tags":        {"id", "name", "post_count"},
}

// keyColumns 是 upsert 的冲突键（两库都有唯一约束，搬运动幂等）。
var keyColumns = map[string][]string{
	"collections": {"id"},
	"posts":       {"id"},
	"users":       {"id"},
	"tags":        {"id"},
}

// CopyColumns 返回某表的拷贝列顺序。
func CopyColumns(table string) []string { return copyColumns[table] }

// KeyColumns 返回某表的 upsert 键列。
func KeyColumns(table string) []string { return keyColumns[table] }
