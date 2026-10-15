// Command blog-go 是个人博客后端 API 的 Go 重写版（对齐旧实现 server/src）。
//
// 运行形态：
//
//	blog-go                  启动 API + 托管嵌入式站点（默认）
//	blog-go migrate-data …   跨库搬运（SQLite ⇄ MySQL），源库只读
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	bloggo "blog-go"
	"blog-go/internal/auth"
	"blog-go/internal/bcrypt"
	"blog-go/internal/collections"
	"blog-go/internal/config"
	"blog-go/internal/httpapi"
	"blog-go/internal/mysql"
	"blog-go/internal/posts"
	"blog-go/internal/snowflake"
	"blog-go/internal/sqlite"
	"blog-go/internal/store"
	"blog-go/internal/tags"
)

func main() {
	if err := run(os.Args[1:], os.Getenv); err != nil {
		fmt.Fprintf(os.Stderr, "启动失败：%v\n", err)
		os.Exit(1)
	}
}

// run 解析子命令并分派；get 便于测试注入环境变量。
func run(args []string, get func(string) string) error {
	logger := newLogger()
	if len(args) > 0 && args[0] == "migrate-data" {
		return runMigrateData(args[1:], logger)
	}
	for _, arg := range args {
		if arg != "" && arg != "serve" {
			return fmt.Errorf("未知参数 %q（可用：serve、migrate-data）", arg)
		}
	}
	cfg, err := config.Load(get)
	if err != nil {
		return err
	}
	return serve(cfg, logger)
}

// serve 装配并启动服务：开库 → 种子 → 监听 → 等退出信号。
func serve(cfg *config.Config, logger *slog.Logger) error {
	ctx := context.Background()
	for _, warning := range cfg.Warnings {
		logger.Warn(warning)
	}
	if err := ensureDirs(cfg); err != nil {
		return err
	}
	dialect, dsn, err := buildDialect(cfg)
	if err != nil {
		return err
	}
	gen := snowflake.New(cfg.SnowflakeWorkerID)
	db, err := store.Open(ctx, dialect, dsn, maxOpenConns(cfg.DBDriver), gen)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	if err := bootstrap(ctx, cfg, db); err != nil {
		return err
	}

	authenticator := auth.New(cfg, db)
	defer func() { _ = authenticator.Close() }()

	postsRepo := posts.New(db, gen)
	collectionsRepo := collections.New(db, gen, postsRepo)
	handler := httpapi.New(httpapi.Options{
		Config:      cfg,
		Auth:        authenticator,
		Posts:       postsRepo,
		Collections: collectionsRepo,
		StaticFS:    bloggo.DistFS(),
		UploadDir:   cfg.UploadDir,
		Logger:      logger,
	})

	server := &http.Server{
		Addr:              net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		Handler:           handler.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return err
	}
	logger.Info("blog-go 已启动: http://" + server.Addr + " | " + cfg.Describe())
	fmt.Println("管理端认证模式: " + authenticator.ModeLabel())
	fmt.Println("博客 API 服务已启动：http://" + server.Addr)
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("服务器错误：" + err.Error())
		}
	}()
	return waitForShutdown(server, logger)
}

// bootstrap 初始化种子数据（对齐旧实现 db.ts 的启动顺序：管理员 → 示例文章 → 重建标签）。
func bootstrap(ctx context.Context, cfg *config.Config, db *store.DB) error {
	generated, created, err := seedUsers(ctx, db, cfg)
	if err != nil {
		return err
	}
	if created {
		if cfg.AdminPassword != "" {
			fmt.Printf("[db] 已按 BLOG_ADMIN_PASSWORD 创建管理员账号：%s\n", cfg.AdminUser)
		} else {
			// 随机口令只打印一次：公开仓库不能内置固定默认口令。
			printGeneratedPassword(cfg.AdminUser, generated)
		}
	}
	seeded, err := posts.Seed(ctx, db)
	if err != nil {
		return err
	}
	if seeded {
		fmt.Println("[db] 已插入 3 篇示例文章")
	}
	if err := tags.Rebuild(ctx, db); err != nil {
		return err
	}
	return nil
}

// seedUsers 在 users 表为空时创建初始管理员；已存在返回 created=false。
// generated 仅在未配置 BLOG_ADMIN_PASSWORD 且实际创建时非空（供一次性打印）。
func seedUsers(ctx context.Context, db *store.DB, cfg *config.Config) (generated string, created bool, err error) {
	var n int64
	if err := db.SQL.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&n); err != nil {
		return "", false, err
	}
	if n > 0 {
		return "", false, nil
	}
	password := cfg.AdminPassword
	if password == "" {
		password, err = randomPassword()
		if err != nil {
			return "", false, err
		}
		generated = password
	}
	hash, err := bcrypt.Hash(password, 10)
	if err != nil {
		return "", false, err
	}
	if _, err := db.SQL.ExecContext(ctx,
		"INSERT INTO users (username, password, created_at) VALUES (?, ?, ?)",
		cfg.AdminUser, hash, store.NowText()); err != nil {
		return "", false, err
	}
	return generated, true, nil
}

// randomPassword 生成 16 位 base64url 口令（对齐旧实现 randomBytes(12).toString('base64url').slice(0,16)）。
func randomPassword() (string, error) {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// printGeneratedPassword 只在首次生成时打印一次，提示立即保存。
func printGeneratedPassword(username, password string) {
	line := strings.Repeat("=", 58)
	fmt.Println("[db] " + line)
	fmt.Printf("[db] 已创建初始管理员账号：%s / %s\n", username, password)
	fmt.Println("[db] 该口令只打印这一次，请立即保存并在后台修改；")
	fmt.Println("[db] 如需自定义，可在首次启动前设置 BLOG_ADMIN_PASSWORD。")
	fmt.Println("[db] " + line)
}

// ensureDirs 创建 SQLite 数据目录与上传目录（MySQL 下只建上传目录）。
func ensureDirs(cfg *config.Config) error {
	if cfg.DBDriver == config.DBDriverSQLite {
		if dir := filepath.Dir(cfg.DBPath); dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return fmt.Errorf("创建数据目录失败: %w", err)
			}
		}
	}
	if cfg.UploadDir != "" {
		if err := os.MkdirAll(cfg.UploadDir, 0o755); err != nil {
			return fmt.Errorf("创建上传目录失败: %w", err)
		}
	}
	return nil
}

// buildDialect 依据配置选方言与 DSN：MySQL 未配置时根本不加载驱动、不建连接。
func buildDialect(cfg *config.Config) (store.Dialect, string, error) {
	switch cfg.DBDriver {
	case config.DBDriverMySQL:
		if cfg.DBDSN != "" {
			return mysql.New(), cfg.DBDSN, nil
		}
		return mysql.New(), mysql.BuildDSN(cfg.DBHost, cfg.DBPort, cfg.DBName, cfg.DBUser, cfg.DBPassword), nil
	case config.DBDriverSQLite:
		return sqlite.New(), sqlite.DSN(cfg.DBPath), nil
	default:
		return nil, "", fmt.Errorf("不支持的 DB_DRIVER %q", cfg.DBDriver)
	}
}

// maxOpenConns 给两种方言各设一个稳妥的连接池上限（SQLite 写锁敏感，池小些）。
func maxOpenConns(driver config.DBDriver) int {
	if driver == config.DBDriverMySQL {
		return 16
	}
	return 4
}

// waitForShutdown 等退出信号，优雅关闭 HTTP 后关闭数据库。
func waitForShutdown(server *http.Server, logger *slog.Logger) error {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	sig := <-signals
	logger.Info("收到 " + signalName(sig) + "，正在退出...")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("关闭服务器超时：" + err.Error())
	}
	logger.Info("已停止。")
	return nil
}

func signalName(sig os.Signal) string {
	switch sig {
	case syscall.SIGINT:
		return "SIGINT"
	case syscall.SIGTERM:
		return "SIGTERM"
	}
	return "信号"
}

// newLogger 用标准库 slog 输出到 stderr。
func newLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

// runMigrateData 执行跨库搬运：--from / --to 用 `sqlite:` / `mysql:` 前缀选驱动。
func runMigrateData(args []string, logger *slog.Logger) error {
	fs := flag.NewFlagSet("migrate-data", flag.ContinueOnError)
	from := fs.String("from", "", "源库 DSN，前缀 sqlite: 或 mysql:")
	to := fs.String("to", "", "目标库 DSN，前缀 sqlite: 或 mysql:")
	dryRun := fs.Bool("dry-run", false, "只统计与校验，不写目标库")
	verify := fs.Bool("verify", false, "搬运后逐表比对条数与抽样行")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *from == "" || *to == "" {
		return errors.New("migrate-data 需要 --from 与 --to")
	}
	srcDialect, srcDSN, err := parseMigrateDSN(*from)
	if err != nil {
		return fmt.Errorf("--from 无效: %w", err)
	}
	dstDialect, dstDSN, err := parseMigrateDSN(*to)
	if err != nil {
		return fmt.Errorf("--to 无效: %w", err)
	}
	ctx := context.Background()
	// 源库用 OpenRaw：只连不迁移，保证只读。
	src, err := store.OpenRaw(ctx, srcDialect, srcDSN, 4)
	if err != nil {
		return fmt.Errorf("打开源库失败: %w", err)
	}
	defer func() { _ = src.Close() }()
	dst, err := store.Open(ctx, dstDialect, dstDSN, 4, nil)
	if err != nil {
		return fmt.Errorf("打开目标库失败: %w", err)
	}
	defer func() { _ = dst.Close() }()

	report, err := store.MigrateData(ctx, src, dst, store.MigrateOptions{DryRun: *dryRun, Verify: *verify})
	if err != nil {
		return err
	}
	printMigrateReport(report, *from, *to)
	if *verify && !report.OK() {
		return errors.New("抽样/条数比对未通过")
	}
	logger.Info("migrate-data 完成")
	return nil
}

// parseMigrateDSN 解析 `sqlite:<path>` / `mysql:<go-sql-driver DSN>`，前缀决定方言。
func parseMigrateDSN(raw string) (store.Dialect, string, error) {
	switch {
	case strings.HasPrefix(raw, "sqlite:"):
		p := strings.TrimPrefix(raw, "sqlite:")
		if strings.TrimSpace(p) == "" {
			return nil, "", errors.New("sqlite DSN 需要文件路径，例如 sqlite:./data/blog.db")
		}
		return sqlite.New(), sqlite.DSN(p), nil
	case strings.HasPrefix(raw, "mysql:"):
		dsn := strings.TrimPrefix(raw, "mysql:")
		if strings.TrimSpace(dsn) == "" {
			return nil, "", errors.New("mysql DSN 不能为空")
		}
		return mysql.New(), dsn, nil
	default:
		return nil, "", fmt.Errorf("无法识别的 DSN %q（需以 sqlite: 或 mysql: 开头）", raw)
	}
}

// printMigrateReport 打印逐表搬运与校验结果（不含任何连接串/口令）。
func printMigrateReport(report *store.MigrateReport, from, to string) {
	mode := "搬运"
	if report.DryRun {
		mode = "演练（dry-run，不写目标库）"
	}
	fmt.Printf("migrate-data %s: %s → %s\n", mode, from, to)
	var totalSrc, totalWritten int64
	for _, t := range report.Tables {
		totalSrc += t.Source
		totalWritten += t.Written
		line := fmt.Sprintf("  %-12s 源 %5d  写入 %5d", t.Table, t.Source, t.Written)
		if report.Verify {
			status := "OK"
			if len(t.Mismatches) > 0 {
				status = "不一致"
			}
			line += fmt.Sprintf("  抽样 %d  %s", t.SampleRows, status)
		}
		fmt.Println(line)
		for _, m := range t.Mismatches {
			fmt.Println("      - " + m)
		}
	}
	fmt.Printf("  合计 源 %d 行，写入 %d 行\n", totalSrc, totalWritten)
}
