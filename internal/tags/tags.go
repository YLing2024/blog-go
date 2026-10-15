// Package tags 负责标签的解析与统计表重建，逻辑逐条对齐旧实现 db.ts。
//
// 标签的权威数据是 posts.tags（逗号分隔字符串）；tags 表只做统计与展示，
// 每次文章增删改后整表重建（旧实现即如此，简单可靠、不怕历史脏数据）。
package tags

import (
	"context"
	"strings"

	"blog-go/internal/store"
)

// Tag 是标签表对外形态。
type Tag struct {
	Name      string `json:"name"`
	PostCount int64  `json:"post_count"`
}

// Parse 把逗号分隔的标签串解析成数组：去首尾空白、去空项、去重（保留首次出现顺序）。
func Parse(raw string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0)
	for _, part := range strings.Split(raw, ",") {
		name := strings.TrimSpace(part)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

// Rebuild 依据 posts.tags 重建 tags 表（按文章数计数）。整体在一个事务里完成，
// 与旧实现一致：先清空再逐项写入。
func Rebuild(ctx context.Context, db *store.DB) error {
	return db.Tx(ctx, func(q store.Querier) error {
		rows, err := q.QueryContext(ctx, "SELECT tags FROM posts")
		if err != nil {
			return err
		}
		counter := map[string]int64{}
		for rows.Next() {
			var raw string
			if err := rows.Scan(&raw); err != nil {
				rows.Close()
				return err
			}
			for _, name := range Parse(raw) {
				counter[name]++
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()

		if _, err := q.ExecContext(ctx, "DELETE FROM tags"); err != nil {
			return err
		}
		for name, count := range counter {
			if _, err := q.ExecContext(ctx,
				"INSERT INTO tags (name, post_count) VALUES (?, ?)", name, count); err != nil {
				return err
			}
		}
		return nil
	})
}

// List 返回全部标签，按文章数降序、名称升序（旧实现 listTags 口径）。
func List(ctx context.Context, db *store.DB) ([]Tag, error) {
	rows, err := db.SQL.QueryContext(ctx,
		"SELECT name, post_count FROM tags ORDER BY post_count DESC, name ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Tag, 0)
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.Name, &t.PostCount); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
