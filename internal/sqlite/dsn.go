package sqlite

// DSN 组装 modernc.org/sqlite 的连接串。
//
// 为与旧实现 db.ts 的 pragma 口径一致（WAL、foreign_keys），并补 synchronous(NORMAL) 与
// busy_timeout 以避免多连接并发下偶发 "database is locked"，这里用 `_pragma=` 参数逐条注入。
// 文件路径按原样拼进 `file:` URI：调用方负责给出合法路径（相对路径相对进程工作目录）。
func DSN(path string) string {
	return "file:" + path +
		"?_pragma=journal_mode(WAL)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_pragma=foreign_keys(ON)" +
		"&_pragma=busy_timeout(5000)"
}
