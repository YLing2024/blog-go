# blog-go

个人博客后端 API 的 **Go 重写版**。行为逐条对齐旧实现（`/root/proj/blog/server`，Node 24 + Express 4 + SQLite），
默认监听 `127.0.0.1:19255`（旧实现的 `4000` 不动，两者并存）。

- **单二进制**：站点前端产物经 `//go:embed` 打进二进制，无外部运行时依赖（SQLite 为纯 Go 实现，无需 cgo）。
- **双库支持**：`DB_DRIVER=sqlite`（默认）或 `mysql`，配置切换；`migrate-data` 子命令做跨库搬运与逐表校验。
- **双认证模式**：`builtin` 自带账号口令 + cookie 会话；`sso` 只信任网关注入的 `X-Auth-User`。

## 构建与运行

需要 Go 1.26+ 与 Node 24+（仅在需要重新构建站点前端产物时用到）。

```bash
make build          # 构建前端产物 + 编译二进制（./blog-go）
make test           # go test -count=1
make race           # 关键路径加 -race
make vet            # go vet
make check          # 聚合门禁：路由↔文档机检 + go vet + go test（退出码 0 才算过）

make run            # 构建后直接运行（读环境变量）
./blog-go migrate-data --help   # 跨库搬运
```

首次启动会自动建库建表、写入 3 篇示例文章；若未设置 `BLOG_ADMIN_PASSWORD`，
会随机生成 16 位初始管理员口令并**只打印一次**，请立即保存。
想自己指定，就在首次启动前设置 `BLOG_ADMIN_USER` 与 `BLOG_ADMIN_PASSWORD`。

## 配置（环境变量）

配置**只读环境变量**（对齐旧实现 systemd `EnvironmentFile` 的部署口径），不读 YAML / `.env` 文件。
键名与默认值逐条对齐旧实现；唯一新增的是数据库选择键（`DB_DRIVER` 等，见 `docs/DB.md`）与
`HOST` / `UPLOAD_DIR`。全部键见 `.env.example`。

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `AUTH_MODE` | `builtin` | 管理端认证模式；未设置或非法值一律回退 `builtin` |
| `HOST` | `127.0.0.1` | 监听地址（对外由 nginx 反代） |
| `PORT` | `19255` | 监听端口（旧实现为 `4000`） |
| `JWT_SECRET` | `dev-secret` | JWT 签名密钥，生产必须显式设置 |
| `DB_DRIVER` | `sqlite` | `sqlite` / `mysql` |
| `DB_PATH` | `./data/blog.db` | SQLite 文件路径（仅 sqlite 模式） |
| `DB_HOST` / `DB_PORT` / `DB_NAME` / `DB_USER` / `DB_PASSWORD` | `3306` / 空 | MySQL 连接参数（仅 mysql 模式） |
| `DB_DSN` | 空 | 直接给出完整 DSN，设置后忽略上面的分段参数 |
| `ADMIN_REDIS_URL` | `redis://127.0.0.1:6379` | 会话校验用的 Redis；不可用时降级 |
| `ADMIN_REDIS_PREFIX` | `admin:session:` | 会话 key 前缀，支持逗号分隔多前缀 |
| `PREVIEW_TTL` | `30m` | 草稿预览令牌有效期（`s/m/h/d/w` 或纯秒数） |
| `PUBLIC_SITE_URL` | 空 | 博客前台基址（含协议）；preview-link 用它拼绝对地址，未配置时回退相对路径并打 warn |
| `SNOWFLAKE_WORKER_ID` | `0` | 多实例部署时区分雪花 ID 机器号 |
| `BLOG_ADMIN_USER` | `admin` | 首次启动创建管理员时的用户名 |
| `BLOG_ADMIN_PASSWORD` | 随机 16 位 | 首次启动创建管理员时的口令；未设置则随机并只打印一次 |
| `UPLOAD_DIR` | `./data/uploads` | 上传图片目录（旧实现写死在 `server/uploads`） |

## 目录

```
cmd/blog-go/         进程入口（装配 / 优雅退出 / migrate-data）
internal/
  config/            环境变量解析
  store/             方言无关的业务 SQL 层 + 跨库搬运
  sqlite/ mysql/     两套薄方言（DDL / upsert / 自增回填 / 列探测 / DSN）
  snowflake/ blogs 对外标识（19 位字符串 ID，防精度丢失）
  bcrypt/ blowfish/  纯 Go bcrypt（对齐旧实现 bcryptjs 的编码与校验）
  jwt/               HS256 最小实现（含 preview 声明）
  auth/              双认证模式、cookie 会话、Redis 第二通道
  posts/ collections/ tags/  文章 / 合集 / 标签业务
  httpapi/           HTTP 契约层（标准库 net/http）
web/                 站点前端源码（原样搬入；产物嵌入，dist 不入库）
deploy/              systemd / nginx 样例（只提交，不安装）
docs/API.md          接口契约（权威，机检）
docs/DB.md           双库映射与迁移
```

## 接口契约

前缀 `/api/blog`，共 19 条路由，清单与逐条契约见 `docs/API.md`（权威）。
`scripts/check-api-docs.mjs` 机检「源码路由 ↔ 文档标题」一致，随 `make check` 一起跑。

管理接口里的 `:id` 是数据库自增整数 id；文章的对外标识始终是 `public_id`（雪花 ID 字符串）。

## 数据库

见 `docs/DB.md`。默认 SQLite；`DB_DRIVER=mysql` 需 `DB_HOST`/`DB_NAME`/`DB_USER`（或 `DB_DSN`），
未配置 MySQL 时**完全不加载驱动、不建立连接**。迁移只做增量（禁止 `DROP` / `TRUNCATE` / 重建表），
现网老库原地升级、数据保留。

## 与旧实现的有意差异

见交付报告，要点：

1. 默认端口 `4000` → `19255`（新旧并存）。
2. 默认监听地址显式化为 `HOST`（默认 `127.0.0.1`）。
3. 新增数据库选择（`DB_DRIVER` 等）与 `migrate-data` 子命令（需求书要求）。
4. 新增 `UPLOAD_DIR`（旧实现写死 `server/uploads`）。
5. 站点由本进程直接托管（旧实现由 nginx 承担）；nginx 侧的非站点行为（认证网关、其它站点反代）不复刻。

## 许可

MIT（与旧仓库一致），见 `LICENSE`。
