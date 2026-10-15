// Package config 负责 blog-go 的环境变量解析与校验。
//
// 键名与默认值逐条对齐旧实现 server/src/：AUTH_MODE / PORT / DB_PATH / JWT_SECRET /
// BLOG_ADMIN_USER / BLOG_ADMIN_PASSWORD / ADMIN_REDIS_URL / ADMIN_REDIS_PREFIX /
// PUBLIC_SITE_URL / PREVIEW_TTL / SNOWFLAKE_WORKER_ID；非法 AUTH_MODE 回落 builtin。
// 本服务端口改为 19255（新旧并存，旧 4000 不动），另新增数据库选择键（见 docs/DB.md）。
package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// AuthMode 管理端认证模式。
type AuthMode string

const (
	// AuthBuiltin 自带账号 + 登录页 + cookie 会话（默认）。
	AuthBuiltin AuthMode = "builtin"
	// AuthSSO 关掉自带口令，管理端身份只认网关注入的 X-Auth-User。
	AuthSSO AuthMode = "sso"
)

// DBDriver 数据库方言。
type DBDriver string

const (
	// DBDriverSQLite 纯 Go SQLite（默认，零额外配置）。
	DBDriverSQLite DBDriver = "sqlite"
	// DBDriverMySQL 需 DB_HOST/DB_PORT/DB_NAME/DB_USER/DB_PASSWORD 或一条 DB_DSN。
	DBDriverMySQL DBDriver = "mysql"
)

// Config 是运行时配置快照。
type Config struct {
	Host string
	Port int

	AuthMode      AuthMode
	JWTSecret     string
	AdminUser     string
	AdminPassword string

	DBDriver   DBDriver
	DBPath     string
	DBHost     string
	DBPort     int
	DBName     string
	DBUser     string
	DBPassword string
	DBDSN      string

	AdminRedisURL    string
	AdminRedisPrefix string

	PublicSiteURL string
	PreviewTTL    time.Duration

	SnowflakeWorkerID int

	// UploadDir 是图片上传目录（旧实现写死在 server/src/uploads；本服务改为可配置）。
	UploadDir string

	// Warnings 收集非致命告警（如 AUTH_MODE 非法回落），由入口统一打印。
	Warnings []string
}

// ConfigError 表示配置错误，Message 可直接展示给运维（不含敏感值）。
type ConfigError struct{ Message string }

// Error 实现 error。
func (e *ConfigError) Error() string { return e.Message }

// Load 从环境变量解析配置；get 便于测试注入。
func Load(get func(string) string) (*Config, error) {
	port, err := pickInt(get("PORT"), "PORT", 19255, 1, 65535)
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		Host:              trimOr(get("HOST"), "127.0.0.1"),
		Port:              port,
		JWTSecret:         trimOr(get("JWT_SECRET"), "dev-secret"),
		AdminUser:         trimOr(get("BLOG_ADMIN_USER"), "admin"),
		AdminPassword:     strings.TrimSpace(get("BLOG_ADMIN_PASSWORD")),
		AdminRedisURL:     trimOr(get("ADMIN_REDIS_URL"), "redis://127.0.0.1:6379"),
		AdminRedisPrefix:  trimOr(get("ADMIN_REDIS_PREFIX"), "admin:session:"),
		PublicSiteURL:     strings.TrimRight(strings.TrimSpace(get("PUBLIC_SITE_URL")), "/"),
		UploadDir:         trimOr(get("UPLOAD_DIR"), "./data/uploads"),
		PreviewTTL:        time.Minute * 30,
		DBDriver:          DBDriverSQLite,
		DBPath:            trimOr(get("DB_PATH"), "./data/blog.db"),
		SnowflakeWorkerID: 0,
	}
	cfg.AuthMode = normalizeAuthMode(get("AUTH_MODE"), &cfg.Warnings)

	ttl, err := parseTTL(trimOr(get("PREVIEW_TTL"), "30m"))
	if err != nil {
		return nil, err
	}
	cfg.PreviewTTL = ttl

	worker, err := pickInt(get("SNOWFLAKE_WORKER_ID"), "SNOWFLAKE_WORKER_ID", 0, 0, 1023)
	if err != nil {
		return nil, err
	}
	cfg.SnowflakeWorkerID = worker

	if err := loadDB(cfg, get); err != nil {
		return nil, err
	}
	return cfg, nil
}

// normalizeAuthMode 解析认证模式：未设置/非法值一律按 builtin，非法值附加告警。
func normalizeAuthMode(raw string, warnings *[]string) AuthMode {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "sso":
		return AuthSSO
	case "builtin", "":
		return AuthBuiltin
	default:
		*warnings = append(*warnings, fmt.Sprintf("AUTH_MODE 取值非法（%s），回退 builtin", value))
		return AuthBuiltin
	}
}

// loadDB 解析数据库选择：默认 sqlite；只有 DB_DRIVER=mysql 时才读取 MySQL 连接参数。
func loadDB(cfg *Config, get func(string) string) error {
	raw := strings.ToLower(strings.TrimSpace(get("DB_DRIVER")))
	switch raw {
	case "", "sqlite":
		cfg.DBDriver = DBDriverSQLite
		return nil
	case "mysql":
		cfg.DBDriver = DBDriverMySQL
		dsn := strings.TrimSpace(get("DB_DSN"))
		if dsn != "" {
			cfg.DBDSN = dsn
			return nil
		}
		cfg.DBHost = strings.TrimSpace(get("DB_HOST"))
		cfg.DBName = strings.TrimSpace(get("DB_NAME"))
		cfg.DBUser = strings.TrimSpace(get("DB_USER"))
		cfg.DBPassword = get("DB_PASSWORD")
		if cfg.DBHost == "" || cfg.DBName == "" || cfg.DBUser == "" {
			return &ConfigError{Message: "DB_DRIVER=mysql 需要 DB_HOST/DB_NAME/DB_USER（或一条 DB_DSN）"}
		}
		port, err := pickInt(get("DB_PORT"), "DB_PORT", 3306, 1, 65535)
		if err != nil {
			return err
		}
		cfg.DBPort = port
		return nil
	default:
		return &ConfigError{Message: fmt.Sprintf("DB_DRIVER 只能是 sqlite / mysql，当前为「%s」", raw)}
	}
}

// parseTTL 解析 jsonwebtoken 风格时长：纯数字按秒；带单位支持 s/m/h/d/w。
func parseTTL(raw string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 30 * time.Minute, nil
	}
	if n, err := strconv.Atoi(raw); err == nil {
		return time.Duration(n) * time.Second, nil
	}
	unit := raw[len(raw)-1]
	number := raw[:len(raw)-1]
	mult := time.Duration(0)
	switch unit {
	case 's':
		mult = time.Second
	case 'm':
		mult = time.Minute
	case 'h':
		mult = time.Hour
	case 'd':
		mult = 24 * time.Hour
	case 'w':
		mult = 7 * 24 * time.Hour
	default:
		return 0, &ConfigError{Message: fmt.Sprintf("PREVIEW_TTL 单位非法：%s（支持 s/m/h/d/w 或纯秒数）", raw)}
	}
	n, err := strconv.Atoi(number)
	if err != nil || n < 0 {
		return 0, &ConfigError{Message: fmt.Sprintf("PREVIEW_TTL 数值非法：%s", raw)}
	}
	return time.Duration(n) * mult, nil
}

// pickInt 读取十进制整数；未设置用默认值，超出取值域直接报错。
func pickInt(raw, name string, fallback, min, max int) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < min || value > max {
		return 0, &ConfigError{Message: fmt.Sprintf("环境变量 %s 必须是 %d–%d 之间的整数，当前为「%s」", name, min, max, raw)}
	}
	return value, nil
}

// trimOr 返回去空白后的值，空则回退默认。
func trimOr(raw, fallback string) string {
	if v := strings.TrimSpace(raw); v != "" {
		return v
	}
	return fallback
}

// Describe 生成启动摘要（不含任何敏感值）。
func (c *Config) Describe() string {
	return fmt.Sprintf("监听 http://%s:%d | 认证模式 %s | 数据库 %s", c.Host, c.Port, c.AuthMode, c.databaseDesc())
}

// databaseDesc 输出不含口令的库描述。
func (c *Config) databaseDesc() string {
	if c.DBDriver == DBDriverMySQL {
		if c.DBDSN != "" || c.DBName != "" {
			name := c.DBName
			if name == "" {
				name = "(DB_DSN)"
			}
			return "mysql " + name
		}
		return "mysql"
	}
	return "sqlite " + c.DBPath
}
