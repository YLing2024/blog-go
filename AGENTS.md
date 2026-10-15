# AGENTS.md — blog-go 维护约定

本仓库是博客后端 API 的 Go 重写。旧实现 `/root/proj/blog/server` 是**功能权威**（只读，不得修改）。
权威工程规范见 `/root/proj/ENGINEERING_BASELINE.md`；本文件只做本仓库补充，不降低其要求。

## 技术栈（固定）

- HTTP：仅标准库 `net/http`；不引 Web 框架与路由库（分发顺序手写在 `internal/httpapi/routes.go`）。
- 数据库：`modernc.org/sqlite`（纯 Go、无 cgo）+ `github.com/go-sql-driver/mysql`；业务 SQL 全收在 `internal/store`。
- Redis：`github.com/redis/go-redis/v9`（会话第二通道；未配置时降级，不影响启动）。
- 口令：`internal/bcrypt` 纯 Go 移植（对齐旧实现 `bcryptjs` 的 `$2a$` 编码与校验）；不引 `x/crypto`。
- JWT：`internal/jwt` 标准库自实现 HS256（含 `exp` / `nbf` / `preview` 声明）。
- 日志：标准库 `log/slog`。
- 前端：`web/` 原样搬入（站点源码来自生产前台 `homepage`）；产物经根包 `//go:embed all:web/dist` 打进二进制。

**允许的第三方依赖只有 `modernc.org/sqlite`、`github.com/go-sql-driver/mysql`、`github.com/redis/go-redis/v9`**；
新增依赖前先改本文件并说明。

## 目录

- `cmd/blog-go/`：进程入口（装配 + 优雅退出 + `migrate-data`）。
- `internal/store/`：方言无关的业务 SQL 层 + 跨库搬运。
- `internal/sqlite/`、`internal/mysql/`：两套薄方言（DDL / upsert / 自增回填 / 列探测 / DSN）。
- `internal/httpapi/`：HTTP 契约层（分发顺序、静态托管、各路由）。
- `internal/{config,snowflake,bcrypt,blowfish,jwt,auth,posts,collections,tags}/`：业务与工具。
- `web/`：站点前端源码（原样搬入；`dist` 与 `node_modules` 不入库）。
- `deploy/`：systemd / nginx 样例（**只提交，不安装到 `/etc`**）。

## 三条硬约束（改动前必读）

1. **HTTP 契约逐条对齐**：路径、方法、状态码、响应体形状（错误统一 `{message}`、公开列表
   `{list,page,pageSize,total,totalPages}`、`/auth-mode` 返回 `{authMode}`、删除 `{ok:true,message}`）不得改；
   管理路由 `:id` 是数据库整数 id，公开 URL 用 `public_id`（雪花 ID **字符串**，绝不可转 number 丢失精度）。
   改了路由必须同步 `docs/API.md`，并由 `scripts/check-api-docs.mjs` 机检通过。
2. **认证双模式语义**：`builtin` 自带账号 + `admin_session` cookie / `Authorization: Bearer`，
   **忽略**外部 `X-Auth-User`；`sso` 只认网关注入的 `X-Auth-User`（缺失/空 → 401），
   `login` / `logout` / `me` 一律 404。`GET /api/blog/auth-mode` 在两种模式下都必须可用。
   旧实现**没有**登录限速，本服务同样不引入。
3. **数据库迁移只做增量**：禁止 `DROP` / `TRUNCATE` / 重建表；`public_id` 回填幂等且放在迁移之后、
   种子之前（使首启示例文章 `public_id` 为空、二次启动回填，与旧实现一致）；两种方言下通过同一套测试。

## 红线

- 源码 / 配置 / 文档 / 提交信息**绝不**出现真实域名、公网 / 内网 IP、邮箱、密钥、真实口令；
  示例统一 `example.com` / `127.0.0.1`。
  （例外：站点源码 `web/` 原样搬入，其中的站点文案属于被托管内容，不做改动。）
- 不写调试残留（`cmd/` 首次生成口令的一次性打印除外）；不留注释掉的死代码；新增 TODO/FIXME 必须为 0；
  禁止 `any` 逃逸、禁止空 `catch`。
- 口令哈希、会话 token、Redis key 值、数据库口令、JWT 密钥一律不进日志。
- 只监听 `127.0.0.1`；生产端口 `19255`。

## 命令

- `make build` / `make test` / `make race` / `make vet` / `make check`（机检路由文档 + vet + 单测）。
- 依赖安装与前端命令一律带 `timeout`。

## 提交

小步提交、中文信息、不带署名与 TASK 号；**禁止** `git push`、`checkout -b`、`rebase`、
`reset --hard`、`merge`；不要 `git add` 运行产物（二进制、`web/dist`、`web/node_modules`、数据目录、数据库文件）。
