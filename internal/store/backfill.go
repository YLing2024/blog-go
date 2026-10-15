package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"blog-go/internal/snowflake"
)

// backfillTables 是需要回填 public_id 的表（对齐旧实现 db.ts 的 ensurePublicId 调用）。
var backfillTables = []string{"posts", "collections"}

// backfillPublicIDs 为历史数据补齐 public_id（雪花 ID）：用 created_at 作为时间源，
// 让老数据的 ID 大致反映其创建时间；同毫秒内靠序列位区分。幂等（只补空值）。
func backfillPublicIDs(ctx context.Context, db *sql.DB, gen *snowflake.Generator) error {
	for _, table := range backfillTables {
		if err := backfillTable(ctx, db, gen, table); err != nil {
			return err
		}
	}
	return nil
}

func backfillTable(ctx context.Context, db *sql.DB, gen *snowflake.Generator, table string) error {
	// table 为受控常量，无注入面。
	rows, err := db.QueryContext(ctx,
		"SELECT id, created_at FROM "+table+" WHERE public_id IS NULL OR public_id = '' ORDER BY id")
	if err != nil {
		return fmt.Errorf("读取待回填的 %s 失败: %w", table, err)
	}
	type pending struct {
		id        int64
		createdAt sql.NullString
	}
	var list []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.createdAt); err != nil {
			rows.Close()
			return err
		}
		list = append(list, p)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if len(list) == 0 {
		return nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	upd, err := tx.PrepareContext(ctx, "UPDATE "+table+" SET public_id = ? WHERE id = ?")
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	for _, p := range list {
		if _, err := upd.ExecContext(ctx, gen.NextAt(parseLocalMillis(p.createdAt.String)), p.id); err != nil {
			_ = upd.Close()
			_ = tx.Rollback()
			return fmt.Errorf("回填 %s.public_id 失败: %w", table, err)
		}
	}
	if err := upd.Close(); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// parseLocalMillis 把旧库的本地时间文本（YYYY-MM-DD HH:MM:SS）解析成毫秒；无法解析时用当前时间。
func parseLocalMillis(text string) int64 {
	t, err := time.ParseInLocation("2006-01-02 15:04:05", text, time.Local)
	if err != nil {
		return time.Now().UnixMilli()
	}
	return t.UnixMilli()
}
