# 数据库（DB.md）

本服务**只支持 MySQL**（2026-10-16 口径变更，不再支持 SQLite）。
表结构**逐条对齐** `go-rewrite/docs/mysql-schema-cloud.md` 的云库 `blog`：表名 / 列名 / 列类型 /
索引名一致，不得另立一套。旧 SQLite 数据由独立工具 `tools/migrate-sqlite-to-mysql/` 一次性搬运。

## 1. 连接配置（环境变量）

| 环境变量 | 默认 | 说明 |
|---|---|---|
| `DB_HOST` | `127.0.0.1` | MySQL 主机 |
| `DB_PORT` | `3306` | MySQL 端口 |
| `DB_NAME` | 空 | 库名（必填，或给 `DB_DSN`） |
| `DB_USER` | 空 | 用户（必填，或给 `DB_DSN`） |
| `DB_PASSWORD` | 空 | 口令（只从环境注入，仓库不提交真实值） |
| `DB_DSN` | 空 | 直接给出 go-sql-driver 完整 DSN；设置后忽略上面的分段参数 |

既无 `DB_DSN` 也无 `DB_NAME`+`DB_USER` 时启动即报错退出。
连接串由 `internal/store.BuildDSN` 组装（`utf8mb4` + `utf8mb4_0900_ai_ci`、`parseTime=false`、
建连/读写超时）；**没有** `DB_DRIVER` 开关，也不加载任何 SQLite 驱动。

## 2. 表结构（对齐云库 `blog`）

四张业务表 + 一张迁移版本表（`schema_version`，本服务的迁移记账，不影响业务表）。
时间列统一为**本地时区**文本 `YYYY-MM-DD HH:MM:SS`，由应用通过 `store.NowText()` 写入
（MySQL 侧是 `longtext` 文本列，不交给驱动解析）。

| 表 | 列（类型） |
|---|---|
| collections | `id` bigint PK AI；`name` longtext；`slug` varchar(256) 唯一；`description` longtext；`created_at` longtext；`public_id` varchar(256) 唯一 |
| posts | `id` bigint PK AI；`title` longtext；`slug` varchar(256) 唯一；`content` longtext；`excerpt` longtext；`tags` longtext；`published` bigint；`created_at`/`updated_at` longtext；`collection_id` bigint 可空；`public_id` varchar(256) 唯一；`subtitle` longtext |
| tags | `id` bigint PK AI；`name` varchar(256) 唯一；`post_count` bigint |
| users | `id` bigint PK AI；`username` varchar(256) 唯一；`password` longtext；`created_at` longtext |
| schema_version | `version` int PK；`applied_at` bigint |

索引名与云库一致：`uq_collections_slug`、`idx_collections_public_id`（唯一）、`uq_posts_slug`、
`idx_posts_public_id`（唯一）、`uq_tags_name`、`uq_users_username`。

`public_id` 是雪花 ID 的**字符串**形态（19 位），存文本，杜绝精度丢失；
`posts` / `collections` 的对外 URL 标识用它，老数据未回填前为 `NULL`，此时回退 `slug`。

## 3. 迁移策略：只增量

`internal/store.Migrate` 在启动时执行（幂等）：

1. `CREATE TABLE IF NOT EXISTS`（含索引定义），完整语句见 `internal/store/schema.go`；
2. 对 `collections`、`posts` 逐列探测（`information_schema.columns`），缺列才
   `ALTER TABLE ... ADD COLUMN`；
3. `INSERT IGNORE` 记录 `schema_version`。

**绝不** `DROP` / `TRUNCATE` / 重建表 —— 现网/云库老数据必须原地继续可用。
需要补列的历史列（对齐旧实现 `db.ts` 的迁移分支）：

- `posts.collection_id`（老库无关联字段）
- `posts.subtitle`（副标题，独立字段）
- `posts.public_id`、`collections.public_id`（雪花 ID）

`public_id` 的回填放在 `store.Open` 迁移之后：为 `public_id IS NULL OR ''` 的行按 `created_at`
生成雪花 ID（让老数据的 ID 大致反映创建时间），幂等。首启装配顺序与旧实现一致：先迁移+回填，
再灌种子数据，因此**首次启动**生成的示例文章 `public_id` 为空、第二次启动才回填 —— 与旧实现完全相同。

## 4. 一次性迁移工具（独立交付物）

`tools/migrate-sqlite-to-mysql/` 是**独立 Go module**（自带 `modernc.org/sqlite`），
读旧 SQLite 文件（只读）→ 写目标 MySQL 库，**不被服务主程序 import**，不进服务运行时。

```bash
cd tools/migrate-sqlite-to-mysql

# 计划（只读源库，不连目标库）
go run . --sqlite /path/to/blog.db --dry-run

# 搬运（源库只读；目标库建表补齐后逐表拷贝 + 抽样比对）
go run . --sqlite /path/to/blog.db \
  --mysql-host 127.0.0.1 --mysql-port 3306 \
  --mysql-name blog --mysql-user blog --mysql-password '***'

# 只校验
go run . --sqlite /path/to/blog.db --mysql-host 127.0.0.1 --mysql-name blog --mysql-user blog --verify
```

- 逐表流水线：**源计数 → 拷贝 → 目标计数 → 抽样比对**；默认模式可重复执行（upsert 幂等）。
- 源库以 `mode=ro` + `query_only(1)` 打开，只发 `SELECT`，绝不删改旧文件。
- 目标库只做 `CREATE TABLE IF NOT EXISTS`（对齐云库 schema），保留原始主键值。
- 表顺序 `collections → posts → users → tags`。

## 5. 测试口径

`internal/store` 的集成用例读环境变量指定一个临时 MySQL；未提供时跳过（无库环境下
`make check` 仍全绿）。验证时可起一个绑 `127.0.0.1` 高位端口的临时容器，验完即停：

```bash
TEST_DB_HOST=127.0.0.1 TEST_DB_PORT=<port> TEST_DB_NAME=blog_test \
TEST_DB_USER=root TEST_DB_PASSWORD='***' go test ./internal/store -count=1
```
