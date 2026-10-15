# migrate-sqlite-to-mysql

一次性迁移工具：把旧 Node 版（SQLite）的博客数据（users / posts / tags / collections）搬到 MySQL。
跑一次即可，之后不再需要 SQLite。

它是**独立 Go module**（`go.mod` 自带 `modernc.org/sqlite`），**不得被服务主程序 import**：
服务模块只依赖 `github.com/go-sql-driver/mysql`，运行时没有任何 SQLite 路径。

## 用法

```bash
cd tools/migrate-sqlite-to-mysql

# 1) 先看计划（只读源库，不连目标库、不写任何数据）
go run . --sqlite /path/to/blog.db --dry-run

# 2) 搬运（源库只读；目标库建表补齐后逐表拷贝 + 抽样比对）
go run . \
  --sqlite /path/to/blog.db \
  --mysql-host 127.0.0.1 --mysql-port 3306 \
  --mysql-name blog --mysql-user blog --mysql-password '***'

# 3) 只校验（不拷贝，只比对条数与抽样行）
go run . --sqlite /path/to/blog.db --mysql-host 127.0.0.1 --mysql-name blog --mysql-user blog --verify
```

也可以用一条 `--mysql-dsn 'user:pass@tcp(host:3306)/blog?parseTime=false'` 代替分段参数。
口令优先取 `--mysql-password`，其次 `MIGRATE_MYSQL_PASSWORD` 环境变量（避免进 shell 历史）。

## 行为

- **源库只读**：以 `mode=ro` + `query_only(1)` 打开，只发 `SELECT`，绝不删改旧文件。
- 逐表流水线：**源计数 → 拷贝 → 目标计数 → 抽样比对**；默认模式即可重复执行（upsert 幂等）。
- `--dry-run` 只报计划；`--verify` 只校验。
- 目标库只做 `CREATE TABLE IF NOT EXISTS`（表名/列名/列类型/索引名与 `docs/mysql-schema-cloud.md`
  的云库 `blog` 对齐），绝不 `DROP` / `TRUNCATE` / 重建表。
- 保留原始主键值；时间列本就是本地时间文本，按原样搬运。

表与列的对应关系见 `docs/DB.md`。
