// Command migrate-sqlite-to-mysql 是一次性迁移工具：把旧 Node 版（SQLite）的博客数据搬到 MySQL。
//
// 它是独立交付物，**不得被服务主程序 import**（本工具是独立 Go module，服务模块里没有它的依赖；
// 服务主程序只依赖 MySQL 驱动，运行时没有任何 SQLite 路径）。
//
// 三种模式：
//
//	默认        逐表「源计数 → 拷贝 → 目标计数 → 抽样比对」，可重复执行（upsert 幂等）
//	--dry-run   只读源库、只报计划，不连目标库、不写任何数据
//	--verify    只校验：不拷贝，只比对目标库与源库的条数与抽样行
//
// 源库一律以只读方式打开（mode=ro + query_only），绝不删改旧文件。
//
// 表结构（表名/列名/列类型/索引名）与文档 docs/mysql-schema-cloud.md 的云库 `blog` 对齐；本工具的
// 建表语句是服务 internal/store 那份的等价副本（独立 module 无法 import internal 包），改动需两处同步。
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"

	_ "modernc.org/sqlite"
)

// ddl 与 internal/store 的建表语句一致：对齐 docs/mysql-schema-cloud.md 的云库 `blog`。
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
}

// tables 是搬运顺序（先父后子，满足外键）。四张表的 id 均为自增主键，因此这张清单同时是
// 「自增序号延续」的处理清单。逐表列顺序显式给出（与服务建表列一致）。
var tables = []string{"collections", "posts", "users", "tags"}

var columns = map[string][]string{
	"collections": {"id", "name", "slug", "description", "created_at", "public_id"},
	"posts":       {"id", "title", "slug", "content", "excerpt", "tags", "published", "created_at", "updated_at", "collection_id", "public_id", "subtitle"},
	"users":       {"id", "username", "password", "created_at"},
	"tags":        {"id", "name", "post_count"},
}

// keys 是各表 upsert 的冲突键（拷贝可重复执行、幂等）。
var keys = map[string][]string{
	"collections": {"id"},
	"posts":       {"id"},
	"users":       {"id"},
	"tags":        {"id"},
}

type options struct {
	sqlitePath string
	dryRun     bool
	verifyOnly bool
	sample     int

	mysqlDSN string
	host     string
	port     int
	name     string
	user     string
	password string
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "迁移失败："+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("migrate-sqlite-to-mysql", flag.ContinueOnError)
	var opt options
	fs.StringVar(&opt.sqlitePath, "sqlite", "", "旧 SQLite 文件路径（只读打开）")
	fs.StringVar(&opt.mysqlDSN, "mysql-dsn", "", "目标 MySQL 的 go-sql-driver DSN（与分段参数二选一）")
	fs.StringVar(&opt.host, "mysql-host", "127.0.0.1", "目标 MySQL 主机")
	fs.IntVar(&opt.port, "mysql-port", 3306, "目标 MySQL 端口")
	fs.StringVar(&opt.name, "mysql-name", "", "目标 MySQL 库名")
	fs.StringVar(&opt.user, "mysql-user", "", "目标 MySQL 用户")
	fs.StringVar(&opt.password, "mysql-password", "", "目标 MySQL 口令（也可用 MIGRATE_MYSQL_PASSWORD 环境变量）")
	fs.BoolVar(&opt.dryRun, "dry-run", false, "只读源库、只报计划，不写目标库")
	fs.BoolVar(&opt.verifyOnly, "verify", false, "只校验：不拷贝，只比对目标库与源库")
	fs.IntVar(&opt.sample, "sample", 5, "每表抽样比对行数")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if opt.dryRun && opt.verifyOnly {
		return errors.New("--dry-run 与 --verify 不能同时使用")
	}
	if strings.TrimSpace(opt.sqlitePath) == "" {
		return errors.New("需要 --sqlite <旧库文件路径>")
	}
	if opt.sample <= 0 {
		opt.sample = 5
	}
	if opt.password == "" {
		opt.password = os.Getenv("MIGRATE_MYSQL_PASSWORD")
	}

	ctx := context.Background()
	src, err := openSQLiteReadOnly(ctx, opt.sqlitePath)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()

	if opt.dryRun {
		return dryRun(ctx, src, opt)
	}

	dst, err := openMySQL(ctx, opt)
	if err != nil {
		return err
	}
	defer func() { _ = dst.Close() }()

	if opt.verifyOnly {
		return report(ctx, src, dst, opt, true)
	}
	if err := createSchema(ctx, dst); err != nil {
		return err
	}
	return report(ctx, src, dst, opt, false)
}

// openSQLiteReadOnly 以只读方式打开旧库：mode=ro 只读文件，query_only 再从会话层禁止写入。
func openSQLiteReadOnly(ctx context.Context, path string) (*sql.DB, error) {
	dsn := "file:" + path + "?mode=ro&_pragma=query_only(1)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开源库失败: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("连接源库失败: %w", err)
	}
	return db, nil
}

// openMySQL 打开目标库：--mysql-dsn 优先，否则由分段参数拼装。
func openMySQL(ctx context.Context, opt options) (*sql.DB, error) {
	dsn := strings.TrimSpace(opt.mysqlDSN)
	if dsn == "" {
		if strings.TrimSpace(opt.name) == "" || strings.TrimSpace(opt.user) == "" {
			return nil, errors.New("目标 MySQL 需要 --mysql-dsn，或 --mysql-name/--mysql-user")
		}
		cfg := mysql.NewConfig()
		cfg.User = opt.user
		cfg.Passwd = opt.password
		cfg.Net = "tcp"
		cfg.Addr = fmt.Sprintf("%s:%d", opt.host, opt.port)
		cfg.DBName = opt.name
		cfg.ParseTime = false
		cfg.Collation = "utf8mb4_0900_ai_ci"
		cfg.Timeout = 5 * time.Second
		dsn = cfg.FormatDSN()
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开目标库失败: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("连接目标库失败: %w", err)
	}
	return db, nil
}

// createSchema 在目标库建表补齐（CREATE TABLE IF NOT EXISTS，绝不破坏既有数据）。
func createSchema(ctx context.Context, dst *sql.DB) error {
	for _, stmt := range ddl {
		if _, err := dst.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("在目标库建表失败: %w", err)
		}
	}
	return nil
}

// dryRun 只读源库并报计划（每表源行数与合计），不连目标库、不写任何数据。
func dryRun(ctx context.Context, src *sql.DB, opt options) error {
	fmt.Printf("演练（dry-run）：只读源库 %s，不写目标库\n", opt.sqlitePath)
	var total int64
	for _, table := range tables {
		n, err := countRows(ctx, src, table)
		if err != nil {
			return err
		}
		total += n
		fmt.Printf("  %-12s 源 %5d 行\n", table, n)
	}
	fmt.Printf("  合计 源 %d 行\n", total)
	return nil
}

// report 执行「源计数 → （可选）拷贝 → 目标计数 → 抽样比对 → （可选）自增序号延续」。
func report(ctx context.Context, src, dst *sql.DB, opt options, verifyOnly bool) error {
	if verifyOnly {
		fmt.Printf("校验（verify）：源 %s → 目标 MySQL\n", opt.sqlitePath)
	} else {
		fmt.Printf("搬运：源 %s → 目标 MySQL\n", opt.sqlitePath)
	}
	// 自增序号要读源库 sqlite_sequence，且目标侧读值必须走同一连接并关掉 information_schema 缓存，
	// 否则 MySQL 8 会返回迁移前的旧 AUTO_INCREMENT（见 docs/DB.md 第 4 节）。
	seqs, err := sourceSequences(ctx, src)
	if err != nil {
		return err
	}
	aiConn, err := dst.Conn(ctx)
	if err != nil {
		return fmt.Errorf("获取目标库自增读连接失败: %w", err)
	}
	defer func() { _ = aiConn.Close() }()
	if _, err := aiConn.ExecContext(ctx, "SET SESSION information_schema_stats_expiry = 0"); err != nil {
		return fmt.Errorf("关闭目标库 information_schema 统计缓存失败: %w", err)
	}
	autoPlans := make([]autoIncrementPlan, 0, len(tables))
	autoMismatches := 0

	var totalSrc, totalWritten int64
	mismatches := 0
	for _, table := range tables {
		cols := columns[table]
		rows, err := readTable(ctx, src, table, cols)
		if err != nil {
			return err
		}
		srcCount := int64(len(rows))
		written := int64(0)
		if !verifyOnly {
			if err := writeTable(ctx, dst, table, cols, rows); err != nil {
				return err
			}
			written = int64(len(rows))
		}
		dstCount, err := countRows(ctx, dst, table)
		if err != nil {
			return err
		}
		bad, err := sampleCompare(ctx, dst, table, cols, rows, opt.sample)
		if err != nil {
			return err
		}
		totalSrc += srcCount
		totalWritten += written
		status := "OK"
		if dstCount != srcCount || len(bad) > 0 {
			status = "不一致"
			mismatches++
		}
		line := fmt.Sprintf("  %-12s 源 %5d", table, srcCount)
		if !verifyOnly {
			line += fmt.Sprintf("  写入 %5d", written)
		}
		line += fmt.Sprintf("  目标 %5d  抽样 %d  %s", dstCount, min(opt.sample, len(rows)), status)
		fmt.Println(line)
		if dstCount != srcCount {
			fmt.Printf("      - 条数不一致: 源 %d / 目标 %d\n", srcCount, dstCount)
		}
		for _, m := range bad {
			fmt.Println("      - " + m)
		}

		srcSeq, haveSeq := seqs[table]
		plan, err := planAutoIncrement(ctx, aiConn, table, srcSeq, haveSeq, !verifyOnly)
		if err != nil {
			return err
		}
		autoPlans = append(autoPlans, plan)
		if verifyOnly && !plan.ok() {
			autoMismatches++
		}
	}
	if verifyOnly {
		fmt.Printf("  合计 源 %d 行\n", totalSrc)
	} else {
		fmt.Printf("  合计 源 %d 行，写入 %d 行\n", totalSrc, totalWritten)
	}
	printAutoIncrementReport(autoPlans, verifyOnly)
	if mismatches > 0 || autoMismatches > 0 {
		return fmt.Errorf("有 %d 张表条数或抽样不一致、%d 张表自增序号未延续", mismatches, autoMismatches)
	}
	return nil
}

// autoIncrementPlan 记录单表「自增序号延续」的执行或校验结果。
//
// 语义来源：SQLite 把「已用过的最大 id」记在 sqlite_sequence.seq（删除行也不回退），
// 下一条新记录的 id 是 seq+1；而 MySQL 的 AUTO_INCREMENT 本身就表示「下一条 id」。
// 两者差一，故源侧要 +1：只有把目标 AUTO_INCREMENT 抬到 max(源 seq+1, 目标 max(id)+1)，
// 迁移后新建记录才会拿到与旧实现一致的下一个 id，避免 id 回退或复用。
type autoIncrementPlan struct {
	table   string
	haveSeq bool
	srcSeq  int64 // 源库 sqlite_sequence 记录的最大已用 id
	maxID   int64 // 目标当前 max(id)
	before  int64 // 目标当前 AUTO_INCREMENT
	want    int64 // 期望下限
	after   int64 // 调整后（拷贝）或现值（校验）
	raised  bool  // 拷贝模式下是否上调过
}

// ok 是校验模式的判据：目标 AUTO_INCREMENT 已达到延续要求。
func (p autoIncrementPlan) ok() bool { return p.after >= p.want }

// dbQuerier 抽出 *sql.DB 与 *sql.Conn 共有的能力，让自增步骤固定在一条连接上执行。
type dbQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// sourceSequences 读源库 sqlite_sequence（仅含 AUTOINCREMENT 表的库才有该表），返回
// 表名 → 已用过的最大 id。源库没有 sqlite_sequence 表时返回空 map，不是错误——此时
// 目标侧按 max(id)+1 处理。
func sourceSequences(ctx context.Context, src *sql.DB) (map[string]int64, error) {
	seqs := map[string]int64{}
	var n int
	if err := src.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='sqlite_sequence'").Scan(&n); err != nil {
		return nil, fmt.Errorf("探测源库 sqlite_sequence 失败: %w", err)
	}
	if n == 0 {
		return seqs, nil
	}
	rows, err := src.QueryContext(ctx, "SELECT name, seq FROM sqlite_sequence")
	if err != nil {
		return nil, fmt.Errorf("读取源库 sqlite_sequence 失败: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var seq sql.NullInt64
		if err := rows.Scan(&name, &seq); err != nil {
			return nil, fmt.Errorf("扫描源库 sqlite_sequence 失败: %w", err)
		}
		if seq.Valid {
			seqs[name] = seq.Int64
		}
	}
	return seqs, rows.Err()
}

// planAutoIncrement 计算目标表 AUTO_INCREMENT 的期望下限，并在拷贝模式下（apply=true）
// 只升不降地上调：want = max(源 seq+1, 目标 max(id)+1)；源无 seq 条目时退化为目标 max(id)+1。
// 目标现值已不小于 want 时原样保留，绝不覆盖更大的现有值。校验模式只读不写。
func planAutoIncrement(ctx context.Context, q dbQuerier, table string, srcSeq int64, haveSeq, apply bool) (autoIncrementPlan, error) {
	plan := autoIncrementPlan{table: table, haveSeq: haveSeq, srcSeq: srcSeq}
	maxID, err := targetMaxID(ctx, q, table)
	if err != nil {
		return plan, err
	}
	before, err := targetAutoIncrement(ctx, q, table)
	if err != nil {
		return plan, err
	}
	want := maxID + 1
	if haveSeq && srcSeq+1 > want {
		want = srcSeq + 1
	}
	plan.maxID, plan.before, plan.want, plan.after = maxID, before, want, before
	if apply && want > before {
		if _, err := q.ExecContext(ctx, "ALTER TABLE "+table+" AUTO_INCREMENT = "+strconv.FormatInt(want, 10)); err != nil {
			return plan, fmt.Errorf("上调目标表 %s AUTO_INCREMENT 失败: %w", table, err)
		}
		after, err := targetAutoIncrement(ctx, q, table)
		if err != nil {
			return plan, err
		}
		plan.after, plan.raised = after, true
	}
	return plan, nil
}

// targetMaxID 读目标表当前最大 id（空表为 0）。
func targetMaxID(ctx context.Context, q dbQuerier, table string) (int64, error) {
	var n int64
	if err := q.QueryRowContext(ctx, "SELECT COALESCE(MAX(id), 0) FROM "+table).Scan(&n); err != nil {
		return 0, fmt.Errorf("读取目标表 %s 最大 id 失败: %w", table, err)
	}
	return n, nil
}

// targetAutoIncrement 读目标表当前 AUTO_INCREMENT（information_schema，调用方须已关掉其统计缓存）。
func targetAutoIncrement(ctx context.Context, q dbQuerier, table string) (int64, error) {
	var n sql.NullInt64
	err := q.QueryRowContext(ctx,
		"SELECT AUTO_INCREMENT FROM information_schema.TABLES WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?",
		table).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("读取目标表 %s AUTO_INCREMENT 失败: %w", table, err)
	}
	if !n.Valid {
		return targetMaxID(ctx, q, table)
	}
	return n.Int64, nil
}

// printAutoIncrementReport 打印逐表的自增序号延续对照（拷贝）或校验结果（verify）。
func printAutoIncrementReport(plans []autoIncrementPlan, verifyOnly bool) {
	if len(plans) == 0 {
		return
	}
	if verifyOnly {
		fmt.Println("  自增序号校验（源 sqlite_sequence.seq 为旧库已用过的最大 id，下一条应为 seq+1）：")
	} else {
		fmt.Println("  自增序号延续（源 sqlite_sequence.seq 为旧库已用过的最大 id，下一条应为 seq+1）：")
	}
	for _, p := range plans {
		seq := "无条目"
		if p.haveSeq {
			seq = fmt.Sprintf("%d（下一条 %d）", p.srcSeq, p.srcSeq+1)
		}
		if verifyOnly {
			status := "OK"
			if !p.ok() {
				status = fmt.Sprintf("不一致：应 >= %d", p.want)
			}
			fmt.Printf("    %-12s 源 seq %s  目标 AUTO_INCREMENT %d  %s\n", p.table, seq, p.after, status)
			continue
		}
		if p.raised {
			fmt.Printf("    %-12s 源 seq %s  目标 AUTO_INCREMENT %d → %d\n", p.table, seq, p.before, p.after)
		} else {
			fmt.Printf("    %-12s 源 seq %s  目标 AUTO_INCREMENT %d  已达标\n", p.table, seq, p.after)
		}
	}
}

// readTable 读出整表（源库只发 SELECT），[]byte 归一为 string。
func readTable(ctx context.Context, src *sql.DB, table string, cols []string) ([][]any, error) {
	rows, err := src.QueryContext(ctx, "SELECT "+strings.Join(cols, ", ")+" FROM "+table)
	if err != nil {
		return nil, fmt.Errorf("读取源表 %s 失败: %w", table, err)
	}
	defer rows.Close()
	out := make([][]any, 0)
	for rows.Next() {
		raw := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, fmt.Errorf("扫描源表 %s 失败: %w", table, err)
		}
		out = append(out, normalizeRow(raw))
	}
	return out, rows.Err()
}

// normalizeRow 把驱动返回的裸值转成可安全写入 MySQL 的形态（[]byte → string）。
func normalizeRow(raw []any) []any {
	out := make([]any, len(raw))
	for i, v := range raw {
		if b, ok := v.([]byte); ok {
			out[i] = string(b)
			continue
		}
		out[i] = v
	}
	return out
}

// writeTable 单表事务内逐行 upsert（幂等）。保留原始主键值，忠实搬运。
func writeTable(ctx context.Context, dst *sql.DB, table string, cols []string, rows [][]any) error {
	if len(rows) == 0 {
		return nil
	}
	stmt := upsertSQL(table, cols, keys[table])
	tx, err := dst.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启目标表 %s 事务失败: %w", table, err)
	}
	for _, row := range rows {
		if _, err := tx.ExecContext(ctx, stmt, row...); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("写入目标表 %s 失败: %w", table, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交目标表 %s 事务失败: %w", table, err)
	}
	return nil
}

// upsertSQL 生成 MySQL 的 INSERT ... ON DUPLICATE KEY UPDATE（按冲突键更新其余列）。
func upsertSQL(table string, cols, keyCols []string) string {
	placeholders := make([]string, len(cols))
	for i := range placeholders {
		placeholders[i] = "?"
	}
	assignments := make([]string, 0, len(cols))
	for _, c := range cols {
		if contains(keyCols, c) {
			continue
		}
		assignments = append(assignments, c+" = VALUES("+c+")")
	}
	base := "INSERT INTO " + table + " (" + strings.Join(cols, ", ") + ") VALUES (" + strings.Join(placeholders, ", ") + ")"
	if len(assignments) == 0 {
		return base
	}
	return base + " ON DUPLICATE KEY UPDATE " + strings.Join(assignments, ", ")
}

// sampleCompare 取源表前 sample 行，按冲突键读回目标行并逐列比对；返回差异描述。
func sampleCompare(ctx context.Context, dst *sql.DB, table string, cols []string, rows [][]any, sample int) ([]string, error) {
	keyCols := keys[table]
	var bad []string
	for i, srcRow := range rows {
		if i >= sample {
			break
		}
		keyVals := make([]any, len(keyCols))
		for j, k := range keyCols {
			keyVals[j] = srcRow[indexOf(cols, k)]
		}
		where := make([]string, len(keyCols))
		for j, k := range keyCols {
			where[j] = k + " = ?"
		}
		query := "SELECT " + strings.Join(cols, ", ") + " FROM " + table +
			" WHERE " + strings.Join(where, " AND ")
		raw := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for j := range raw {
			ptrs[j] = &raw[j]
		}
		err := dst.QueryRowContext(ctx, query, keyVals...).Scan(ptrs...)
		if errors.Is(err, sql.ErrNoRows) {
			bad = append(bad, fmt.Sprintf("抽样缺行: %s=%v", keyCols[0], keyVals[0]))
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("抽样读取目标表 %s 失败: %w", table, err)
		}
		dstRow := normalizeRow(raw)
		if !rowsEqual(srcRow, dstRow) {
			bad = append(bad, fmt.Sprintf("抽样不一致: %s=%v", keyCols[0], keyVals[0]))
		}
	}
	return bad, nil
}

// rowsEqual 逐列比两行；数值统一按十进制字符串比，消除驱动类型差异。
func rowsEqual(a, b []any) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if scalarString(a[i]) != scalarString(b[i]) {
			return false
		}
	}
	return true
}

// scalarString 把标量归一为字符串：nil → 空，数字去尾零，[]byte → string。
func scalarString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case []byte:
		return string(t)
	case int64:
		return fmt.Sprintf("%d", t)
	case int:
		return fmt.Sprintf("%d", t)
	case float64:
		return fmt.Sprintf("%g", t)
	case bool:
		return fmt.Sprintf("%t", t)
	default:
		return fmt.Sprintf("%v", t)
	}
}

func countRows(ctx context.Context, db *sql.DB, table string) (int64, error) {
	var n int64
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&n); err != nil {
		return 0, fmt.Errorf("统计表 %s 失败: %w", table, err)
	}
	return n, nil
}

func indexOf(list []string, v string) int {
	for i, item := range list {
		if item == v {
			return i
		}
	}
	return -1
}

func contains(list []string, v string) bool { return indexOf(list, v) >= 0 }
