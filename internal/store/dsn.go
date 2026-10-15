package store

import (
	"fmt"
	"time"

	"github.com/go-sql-driver/mysql"
)

// BuildDSN 由分段参数拼出 go-sql-driver 的 DSN。口令只进返回值，绝不落日志。
//
// 参数取舍：utf8mb4 + utf8mb4_0900_ai_ci（与云库表定义一致）；parseTime=false
// （时间列是文本，不交给驱动解析）；timeout 控制建连、read/writeTimeout 防止慢查询挂死。
func BuildDSN(host string, port int, name, user, password string) string {
	cfg := mysql.NewConfig()
	cfg.User = user
	cfg.Passwd = password
	cfg.Net = "tcp"
	cfg.Addr = fmt.Sprintf("%s:%d", host, port)
	cfg.DBName = name
	cfg.ParseTime = false
	cfg.Collation = "utf8mb4_0900_ai_ci"
	cfg.Loc = time.Local
	cfg.Timeout = 5 * time.Second
	cfg.ReadTimeout = 15 * time.Second
	cfg.WriteTimeout = 15 * time.Second
	return cfg.FormatDSN()
}
