# 数据库与跨库搬运（DB.md）

本服务支持 `DB_DRIVER=sqlite`（默认）与 `DB_DRIVER=mysql` 两种存储，业务契约与响应形状完全一致。
本文说明表结构、方言映射、迁移策略与 `migrate-data` 子命令。

## 1. 选择驱动

| 环境变量 | 默认 | 说明 |
|---|---|---|
| `DB_DRIVER` | `sqlite` | `sqlite` / `mysql`；其他值直接报错退出 |
| `DB_PATH` | `./data/blog.db` | SQLite 文件（仅 sqlite 模式） |
| `DB_HOST` / `DB_PORT` / `DB_NAME` / `DB_USER` / `DB_PASSWORD` | `3306` / 空 / 空 / 空 | MySQL 连接参数（仅 mysql 模式） |
| `DB_DSN` | 空 | 直接给出 go-sql-driver 完整 DSN；设置后忽略上面的分段参数 |

`DB_DRIVER=mysql` 时若既无 `DB_DSN` 也无 `DB_HOST`+`DB_NAME`+`DB_USER`，启动即报错并退出。
`DB_DRIVER=sqlite` 时**完全不加载 MySQL 驱动、不建立连接**（dialect 只在需要时才构造）。

## 2. 表结构（两库同构）

四张业务表 + 一张迁移版本表。时间列统一为**本地时区**文本 `YYYY-MM-DD HH:MM:SS`，
由应用通过 `store.NowText()` 写入（两库可逐字对拍；MySQL 侧是 `VARCHAR(32)` 而非 `DATETIME`，
避免驱动/时区把文本再解释一遍）。

| 表 | 列 | SQLite 类型 | MySQL 类型 |
|---|---|---|---|
| collections | id | INTEGER PK AUTOINCREMENT | BIGINT AUTO_INCREMENT PK |
| collections | name | TEXT NOT NULL | VARCHAR(255) NOT NULL |
| collections | slug | TEXT NOT NULL UNIQUE | VARCHAR(255) NOT NULL UNIQUE |
| collections | public_id | TEXT（唯一索引） | VARCHAR(64)（唯一键） |
| collections | description | TEXT NOT NULL DEFAULT '' | TEXT NOT NULL |
| collections | created_at | TEXT NOT NULL DEFAULT (datetime('now','localtime')) | VARCHAR(32) NOT NULL DEFAULT '' |
| users | id / username / password / created_at | INTEGER PK / TEXT UNIQUE NOT NULL / TEXT NOT NULL / TEXT | BIGINT PK / VARCHAR(255) UNIQUE / VARCHAR(255) / VARCHAR(32) |
| posts | id | INTEGER PK AUTOINCREMENT | BIGINT AUTO_INCREMENT PK |
| posts | title | TEXT NOT NULL | VARCHAR(255) NOT NULL |
| posts | subtitle | TEXT NOT NULL DEFAULT '' | VARCHAR(255) NOT NULL DEFAULT '' |
| posts | slug | TEXT NOT NULL UNIQUE | VARCHAR(255) NOT NULL UNIQUE |
| posts | public_id | TEXT（唯一索引） | VARCHAR(64)（唯一键） |
| posts | content | TEXT NOT NULL DEFAULT '' | MEDIUMTEXT NOT NULL |
| posts | excerpt | TEXT NOT NULL DEFAULT '' | TEXT NOT NULL |
| posts | tags | TEXT NOT NULL DEFAULT '' | TEXT NOT NULL |
| posts | published | INTEGER NOT NULL DEFAULT 1 | TINYINT NOT NULL DEFAULT 1 |
| posts | created_at / updated_at | TEXT NOT NULL DEFAULT (datetime('now','localtime')) | VARCHAR(32) NOT NULL DEFAULT '' |
| posts | collection_id | INTEGER REFERENCES collections(id) ON DELETE SET NULL | BIGINT NULL + 外键 ON DELETE SET NULL |
| tags | id / name / post_count | INTEGER PK / TEXT UNIQUE / INTEGER | BIGINT PK / VARCHAR(255) UNIQUE / BIGINT |
| schema_version | version / applied_at | INTEGER PK / INTEGER | INT PK / BIGINT |

`public_id` 是雪花 ID 的**字符串**形态（19 位），两库都存文本，杜绝精度丢失。

## 3. 迁移策略：只增量

`internal/sqlite` 与 `internal/mysql` 的 `Migrate` 都在单个事务里执行：

1. `CREATE TABLE IF NOT EXISTS` / `CREATE UNIQUE INDEX` / `CREATE INDEX`（幂等）；
2. 对 `collections`、`posts` 逐列探测（SQLite 用 `pragma_table_info`，MySQL 用
   `information_schema.columns`），缺列才 `ALTER TABLE ... ADD COLUMN`；
3. `INSERT OR IGNORE` / `INSERT IGNORE` 记录 `schema_version`。

**绝不** `DROP` / `TRUNCATE` / 重建表 —— 现网老库必须原地继续可用。
需要补列的历史列（对齐旧实现 `db.ts` 的迁移分支）：

- `posts.collection_id`（老库无关联字段）
- `posts.subtitle`（副标题，独立字段）
- `posts.public_id`、`collections.public_id`（雪花 ID）

`public_id` 的回填放在 `store.Open` 迁移之后：为 `public_id IS NULL OR ''` 的行按 `created_at`
生成雪花 ID（让老数据的 ID 大致反映创建时间），幂等。首启装配顺序与旧实现一致：先迁移+回填，
再灌种子数据，因此**首次启动**生成的示例文章 `public_id` 为空、第二次启动才回填 —— 与旧实现完全相同。

## 4. 方言差异收口

业务 SQL（`internal/posts`、`internal/collections`、`internal/auth`、`internal/tags`）只用 `?` 占位符，
不感知驱动。差异全部落在 `store.Dialect` 的两个实现：

| 能力 | SQLite | MySQL |
|---|---|---|
| 驱动名 | `sqlite`（modernc.org/sqlite，纯 Go） | `mysql`（go-sql-driver/mysql） |
| 列探测 | `pragma_table_info` | `information_schema.columns` |
| 自增回填 | `INSERT ... RETURNING id` | `Exec` + `LastInsertId()` |
| upsert | `ON CONFLICT(...) DO UPDATE SET c = excluded.c` | `ON DUPLICATE KEY UPDATE c = VALUES(c)` |
| 标签匹配 | `(',' \|\| tags \|\| ',') LIKE ? ESCAPE '\'` | `CONCAT(',', tags, ',') LIKE ? ESCAPE '\\'` |
| 跨库前准备 | `PRAGMA foreign_keys=OFF`（连接级） | `SET FOREIGN_KEY_CHECKS=0`（会话级） |

DSN 由 `internal/sqlite.DSN` / `internal/mysql.BuildDSN` 组装；MySQL 用 `utf8mb4` + `utf8mb4_unicode_ci`、
`parseTime=false`、连接/读写超时。

## 5. `migrate-data` 子命令

把源库逐表**忠实拷贝**到目标库（不是清洗），源库只读（仅 `SELECT`）。

```bash
blog-go migrate-data --from "sqlite:./data/blog.db"     --to "mysql:user:pass@tcp(127.0.0.1:3306)/blog?charset=utf8mb4" --verify
blog-go migrate-data --from "mysql:...dsn..."           --to "sqlite:./data/blog.db" --dry-run
```

| 参数 | 说明 |
|---|---|
| `--from` | 源库 DSN，前缀 `sqlite:`（文件路径）或 `mysql:`（go-sql-driver DSN） |
| `--to` | 目标库 DSN，同上 |
| `--dry-run` | 只统计源库条数，不写目标库 |
| `--verify` | 搬运后逐表比对**条数** + 抽样**行内容**，不一致则非 0 退出 |

行为细节：

- 表顺序 `collections → posts → users → tags`（先父后子，满足外键）。
- 每表单事务 `upsert`（按主键），可重复执行、幂等。
- 写入固定在同一条目标连接上，拷贝前先关外键强制（见上表），因此源库历史遗留的孤儿行会被
  **原样**搬过去，而不是被外键挡下或清洗。
- 校验：条数一致 + 按主键抽样（默认 5 行）逐列比对（数值型表达归一后再比，消除驱动差异）。
- 时间列本就是文本，两库间无需转换。
- **不删、不清、不重建**目标库既有数据；再跑一次是幂等覆盖。

## 6. 验收口径

同一套 `internal/httpapi` 测试与手工请求矩阵在 `sqlite` 与 `mysql`（临时容器）下各跑一遍，
响应体除 `created_at/updated_at`（时间戳）外逐字一致；`migrate-data --verify` 在
「现网 SQLite 副本 → MySQL」与「MySQL → SQLite」两个方向都返回 0。
