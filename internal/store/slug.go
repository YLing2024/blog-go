package store

import (
	"context"
	"database/sql"
	"fmt"
)

// UniqueSlug 为 slug 这一「内部别名」生成不冲突的取值（对齐旧实现 uniqueSlug）：
// 若 base 已被占用（且不是被 excludeID 那行占用），依次尝试 base-2、base-3……
// table 为受控常量；excludeID<=0 表示新建场景（不与任何行自身冲突）。
func UniqueSlug(ctx context.Context, q Querier, table, base string, excludeID int64) (string, error) {
	stmt := "SELECT id FROM " + table + " WHERE slug = ?"
	candidate := base
	for n := 2; ; n++ {
		var id int64
		err := q.QueryRowContext(ctx, stmt, candidate).Scan(&id)
		if err == sql.ErrNoRows {
			return candidate, nil
		}
		if err != nil {
			return "", fmt.Errorf("检查 %s.slug 冲突失败: %w", table, err)
		}
		if excludeID > 0 && id == excludeID {
			return candidate, nil
		}
		candidate = fmt.Sprintf("%s-%d", base, n)
	}
}
