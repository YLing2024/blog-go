# blog-go 接口文档

本文件是博客后端 API 的权威契约文档，与 `internal/httpapi/routes.go` 的 `routeTable` 一一对应，
由 `scripts/check-api-docs.mjs` 机检（`npm`/`node scripts/check-api-docs.mjs`，已并入 `make check`）。
路径、方法、状态码、响应体形状与旧实现 `blog/server`（Express 4）逐条对齐。

- 所有端点都挂在 `/api/blog` 前缀下。
- 请求体一律 `application/json`（图片上传例外，见 `POST /api/blog/admin/upload`）。
- 错误体统一为 `{"message":"..."}`。
- 未匹配到任何 API 路由的 `/api/blog/*` 请求一律返回 `404 {"message":"接口不存在"}`；
  处理过程中抛出的未预期错误返回 `500 {"message":"服务器内部错误"}`。

## 认证模式

鉴权由环境变量 `AUTH_MODE` 决定，默认 `builtin`，非法值回落 `builtin`。

| 取值 | 语义 |
|---|---|
| `builtin` | 自带账号体系。`POST /api/blog/admin/login` 用 `bcrypt` 校验用户口令，签发 7 天 HS256 JWT 并下发 `admin_session` cookie；管理接口接受 `Authorization: Bearer <token>` 或该 cookie。此模式**忽略** `X-Auth-User`，不因外部头提权。 |
| `sso` | 只认网关注入的 `X-Auth-User` 请求头（缺失或空 → `401 {"message":"未登录，请先登录"}`）。`/admin/login`、`/admin/logout`、`/admin/me` 三个自带账号端点一律 `404 {"message":"接口不存在"}`。`auth-mode`、公开读端点不受影响。 |

- `GET /api/blog/auth-mode` 两个模式下都可用（协议类端点，不受 `AUTH_MODE` 影响）。
- 会话 cookie：名 `admin_session`，`Path=/; HttpOnly; SameSite=Lax`，HTTPS（含反代 `X-Forwarded-Proto: https`）下加 `Secure`，`Max-Age=43200`。
- Redis 第二通道（可选）：登录时把 token 写入 `ADMIN_REDIS_PREFIX + token`（TTL 12 小时，校验通过滑动续期）。
  Redis 不可用时降级为纯 JWT（此时登出无法立即失效，属与旧实现相同的已知取舍）。
  未配置 Redis 不影响登录与鉴权。
- **无登录限速**（与旧实现一致，旧实现没有限速逻辑）。

## 数据模型约定

时间一律为**本地时区**文本 `YYYY-MM-DD HH:MM:SS`（SQLite 与 MySQL 同构），由应用写入。

| 实体 | 字段 | 说明 |
|---|---|---|
| post | `id` | 数据库自增整数，**仅管理接口**使用（`/admin/posts/:id`）。 |
| post | `public_id` | 雪花 ID，**字符串**（19 位，超出 JS `Number.MAX_SAFE_INTEGER`，**禁止转 Number**）。对外 URL 标识；老数据未回填前为 `null`，此时回退 `slug`。 |
| post | `title` | 标题（详情页唯一 `h1`）。 |
| post | `subtitle` | 副标题，**独立字段**（不是摘要）。上限 200 字符，超长按 UTF-16 代码单元截断；留空即不显示。 |
| post | `slug` | 内部别名/历史链接兼容，唯一；重名自动加 `-2`、`-3` 后缀，不再是错误。 |
| post | `content` | Markdown 正文。约定：**不得使用 `#` 一级标题**（章节用 `##`）；前端渲染兜底会把正文 `h1` 降级为 `h2`（代码块内保留）。后端按原样存储，不做改写。 |
| post | `excerpt` | 摘要，仅去首尾空白后**原样存储**（旧实现无自动截取）。 |
| post | `tags` | 存储为逗号分隔字符串，对外返回字符串数组。 |
| post | `published` | 布尔；`1`=已发布，`0`=草稿。**新建时缺省为草稿**。 |
| post | `collection` | 关联详情或 `null`；对象含 `id/name/slug/public_id`。 |
| collection | `description` | 合集描述，可为空串。 |
| collection | `public_id` | 同上，合集对外 URL 标识。 |

## 公开读

### GET /api/blog/auth-mode

- 鉴权：无。
- 请求参数：无。
- curl：`curl -s http://127.0.0.1:19255/api/blog/auth-mode`
- 成功响应：`200`，`{"authMode":"builtin"}`（或 `"sso"`）。
- 错误：无。
- 副作用：无。幂等。

### GET /api/blog/posts

- 鉴权：无。
- 请求参数：

  | 名称 | 位置 | 类型 | 必填 | 默认 | 校验 |
  |---|---|---|---|---|---|
  | `page` | query | int | 否 | `1` | 取前导整数；`<1` 或非法回 `1`；大于总页数时夹到最后一页 |
  | `tag` | query | string | 否 | 空 | 去首尾空白；按逗号边界精确匹配标签 |

- curl：`curl -s 'http://127.0.0.1:19255/api/blog/posts?page=1&tag=Go'`
- 成功响应：`200`

  ```json
  {
    "list": [
      {
        "id": 1,
        "title": "标题",
        "subtitle": "",
        "slug": "hello-world",
        "public_id": "2110777709687734272",
        "excerpt": "摘要",
        "tags": ["随笔", "博客"],
        "published": true,
        "created_at": "2026-10-16 00:59:57",
        "updated_at": "2026-10-16 00:59:57",
        "collection": null
      }
    ],
    "page": 1,
    "pageSize": 10,
    "total": 1,
    "totalPages": 1
  }
  ```

  仅返回 `published=1`；按 `created_at DESC, id DESC` 排序；列表不含 `content`。
- 错误：无（查询参数非法已归一）。
- 副作用：无。幂等。

### GET /api/blog/posts/:slug

- 鉴权：可选（软鉴权）。登录态可读草稿；匿名带有效 `?preview=` 令牌亦可读草稿。
- 请求参数：

  | 名称 | 位置 | 类型 | 必填 | 默认 | 校验 |
  |---|---|---|---|---|---|
  | `slug` | path | string | 是 | — | 先按 `public_id` 精确匹配，未命中再按 `slug` |
  | `preview` | query | string | 否 | 空 | 预览 JWT，需未过期且 `preview` 声明等于该 `:slug` |

- curl：`curl -s http://127.0.0.1:19255/api/blog/posts/hello-world`
- 成功响应：`200`，文章详情（在 `public_id` 之后多一个 `content`）：

  ```json
  {
    "id": 1, "title": "标题", "subtitle": "", "slug": "hello-world",
    "public_id": "2110777709687734272", "content": "## 正文",
    "excerpt": "摘要", "tags": [], "published": true,
    "created_at": "2026-10-16 00:59:57", "updated_at": "2026-10-16 00:59:57",
    "collection": null
  }
  ```

- 错误：

  | 状态码 | 条件 | message |
  |---|---|---|
  | `404` | 不存在，或为草稿且无登录态/无有效预览令牌 | `文章不存在或未发布` |

- 副作用：无。幂等。

### GET /api/blog/collections

- 鉴权：无。
- 请求参数：无。
- curl：`curl -s http://127.0.0.1:19255/api/blog/collections`
- 成功响应：`200`

  ```json
  {"list":[{"id":1,"name":"技术","slug":"技术","public_id":"2110777869683654656","description":"描述","created_at":"2026-10-16 01:00:35","post_count":0}]}
  ```

  `post_count` 只计**已发布**文章；按 `created_at ASC, id ASC` 排序。
- 错误：无。
- 副作用：无。幂等。

### GET /api/blog/collections/:slug

- 鉴权：无。
- 请求参数：

  | 名称 | 位置 | 类型 | 必填 | 默认 | 校验 |
  |---|---|---|---|---|---|
  | `slug` | path | string | 是 | — | 先按 `public_id`，未命中再按 `slug` |

- curl：`curl -s http://127.0.0.1:19255/api/blog/collections/技术`
- 成功响应：`200`，合集详情 + 其下**已发布**文章列表：

  ```json
  {"id":1,"name":"技术","slug":"技术","public_id":"2110777869683654656","description":"描述","created_at":"2026-10-16 01:00:35","posts":[]}
  ```

- 错误：

  | 状态码 | 条件 | message |
  |---|---|---|
  | `404` | 合集不存在 | `合集不存在` |

- 副作用：无。幂等。

### GET /api/blog/uploads/:name

- 鉴权：无。
- 请求参数：

  | 名称 | 位置 | 类型 | 必填 | 默认 | 校验 |
  |---|---|---|---|---|---|
  | `name` | path | string | 是 | — | 取 basename；含 `..` 或空 → 400；扩展名须在白名单内 |

- curl：`curl -s -o /tmp/a.png http://127.0.0.1:19255/api/blog/uploads/1792083635184-911162.png`
- 成功响应：`200`，图片原始字节；响应头固定带：
  - `Content-Type`：白名单映射（`image/png` 等）
  - `X-Content-Type-Options: nosniff`
  - `Content-Security-Policy: default-src 'none'; style-src 'unsafe-inline'; sandbox`
- 错误：

  | 状态码 | 条件 | message |
  |---|---|---|
  | `400` | 文件名为空或含 `..` | `非法文件名` |
  | `404` | 扩展名不在白名单，或文件不存在 | `图片不存在` |

- 副作用：无。幂等。

## 自带账号入口

### POST /api/blog/admin/login

- 鉴权：无（`sso` 模式下整个端点 `404`）。
- 请求参数：

  | 名称 | 位置 | 类型 | 必填 | 默认 | 校验 |
  |---|---|---|---|---|---|
  | `username` | body | string | 是 | — | 去首尾空白后查用户；JS 假值（空/null）→ 400 |
  | `password` | body | string | 是 | — | bcrypt 比对 |

- curl：

  ```bash
  curl -s -X POST -H 'Content-Type: application/json' \
    -d '{"username":"admin","password":"由部署环境注入"}' \
    http://127.0.0.1:19255/api/blog/admin/login
  ```

- 成功响应：`200`，并下发 `Set-Cookie: admin_session=...`

  ```json
  {"token":"<jwt>","username":"admin","message":"登录成功"}
  ```

- 错误：

  | 状态码 | 条件 | message |
  |---|---|---|
  | `400` | 账号或口令缺失/为假值 | `请输入账号和密码` |
  | `401` | 用户不存在或口令不符 | `账号或密码错误` |
  | `404` | `sso` 模式 | `接口不存在` |

- 副作用：写 Redis 会话（若可用）；下发 cookie。**无幂等保证**（每次登录签发新 token）。
- 限速/超时：无限速；Redis 写超时 2 秒，失败静默降级。

### POST /api/blog/admin/logout

- 鉴权：无（`sso` 模式下 `404`）。
- 请求参数：无（凭证取自 `Bearer` 或 cookie，二者取一，`Bearer` 优先）。
- curl：`curl -s -X POST http://127.0.0.1:19255/api/blog/admin/logout`
- 成功响应：`200`

  ```json
  {"ok":true,"message":"已退出登录"}
  ```

- 错误：`404`（`sso` 模式）。
- 副作用：删除各前缀下的 Redis 会话 key；清除 cookie。**幂等**（未登录也 200）。

### GET /api/blog/admin/me

- 鉴权：需要（`builtin`；`sso` 模式下 `404`）。
- 请求参数：无。
- curl：`curl -s -H "Authorization: Bearer <jwt>" http://127.0.0.1:19255/api/blog/admin/me`
- 成功响应：`200`

  ```json
  {"name":"admin","role":"admin"}
  ```

  名取 `username`，缺失回退 `admin`。
- 错误：

  | 状态码 | 条件 | message |
  |---|---|---|
  | `401` | 无凭证 | `未登录，请先登录` |
  | `401` | 凭证无效/过期 | `登录已过期，请重新登录` |
  | `404` | `sso` 模式 | `接口不存在` |

- 副作用：无。幂等。

## 管理端文章

以下端点均需鉴权（`builtin` 用 Bearer/cookie；`sso` 用 `X-Auth-User`），未登录返回
`401 {"message":"未登录，请先登录"}` 或 `{"message":"登录已过期，请重新登录"}`。

### GET /api/blog/admin/posts

- 鉴权：需要。
- 请求参数：无。
- curl：`curl -s -H "Authorization: Bearer <jwt>" http://127.0.0.1:19255/api/blog/admin/posts`
- 成功响应：`200`，`{"list":[<文章详情>...]}`（含草稿，按 `updated_at DESC, id DESC`）。
- 错误：`401`（见上）。
- 副作用：无。幂等。

### GET /api/blog/admin/posts/:id/preview-link

- 鉴权：需要。
- 请求参数：

  | 名称 | 位置 | 类型 | 必填 | 默认 | 校验 |
  |---|---|---|---|---|---|
  | `id` | path | int | 是 | — | 数据库自增 id；非整数按 `0`（不命中） |

- curl：`curl -s -H "Authorization: Bearer <jwt>" http://127.0.0.1:19255/api/blog/admin/posts/1/preview-link`
- 成功响应：`200`

  ```json
  {"published":false,"url":"https://example.com/blog/2110777869276807168?preview=<jwt>"}
  ```

  已发布给公开地址；草稿附 `PREVIEW_TTL`（默认 30 分钟）内有效的预览令牌，绑定该文章 key。
  基址取 `PUBLIC_SITE_URL`；未配置时回退相对路径并告警一次。
- 错误：

  | 状态码 | 条件 | message |
  |---|---|---|
  | `401` | 未登录 | 见上 |
  | `404` | 文章不存在 | `文章不存在` |

- 副作用：无（令牌现签，不落库）。幂等。

### POST /api/blog/admin/posts

- 鉴权：需要。
- 请求参数：

  | 名称 | 位置 | 类型 | 必填 | 默认 | 校验 |
  |---|---|---|---|---|---|
  | `title` | body | string | 是 | — | 去首尾空白后不得为空，否则 400 |
  | `slug` | body | string | 否 | 由 `title` 生成 | 冲突自动加序号 |
  | `content` | body | string | 否 | `""` | 原样存储 |
  | `excerpt` | body | string | 否 | `""` | 去首尾空白；不自动截取 |
  | `subtitle` | body | string | 否 | `""` | 去首尾空白，超 200 字符截断 |
  | `tags` | body | string \| string[] | 否 | `[]` | 数组原样参与 join；字符串按逗号拆分去重 |
  | `published` | body | bool | 否 | 草稿（`0`） | 真值即 `1` |
  | `collection_id` | body | int \| null \| "" | 否 | 无关联 | 指定但不存在/非法 → 400 |

- curl：

  ```bash
  curl -s -X POST -H "Authorization: Bearer <jwt>" -H 'Content-Type: application/json' \
    -d '{"title":"标题","content":"## 正文","tags":["Go"],"published":true}' \
    http://127.0.0.1:19255/api/blog/admin/posts
  ```

- 成功响应：`201`，文章详情。
- 错误：

  | 状态码 | 条件 | message |
  |---|---|---|
  | `400` | 标题为空 | `标题不能为空` |
  | `400` | `collection_id` 指定但不存在/非法 | `合集不存在` |
  | `401` | 未登录 | 见上 |

- 副作用：写 `posts`；随后**整表重建** `tags` 统计。非幂等（每次新建一行）。

### PUT /api/blog/admin/posts/:id

- 鉴权：需要。
- 请求参数：

  | 名称 | 位置 | 类型 | 必填 | 默认 | 校验 |
  |---|---|---|---|---|---|
  | `id` | path | int | 是 | — | 非整数按 `0` |
  | `title` | body | string | 否 | 保持原值 | `null` 视为保持原值 |
  | `slug` | body | string | 否 | 保持原值 | 仅在非空时改为新 slug |
  | `content` | body | string | 否 | 保持原值 | `null` 保持 |
  | `excerpt` | body | string | 否 | 保持原值 | `null` 保持 |
  | `subtitle` | body | string | 否 | 保持原值 | 出现即写（`null` → 清空为 `""`） |
  | `tags` | body | string \| string[] | 否 | 保持原值 | `null`/缺失保持 |
  | `published` | body | bool | 否 | 保持原值 | |
  | `collection_id` | body | int \| null \| "" | 否 | 保持原值 | `null`/`""` → 清除；指定不存在 → 400 |

- curl：

  ```bash
  curl -s -X PUT -H "Authorization: Bearer <jwt>" -H 'Content-Type: application/json' \
    -d '{"published":true,"subtitle":"新副标题"}' \
    http://127.0.0.1:19255/api/blog/admin/posts/1
  ```

- 成功响应：`200`，更新后的文章详情（`updated_at` 刷新）。
- 错误：

  | 状态码 | 条件 | message |
  |---|---|---|
  | `400` | `collection_id` 指定但不存在/非法 | `合集不存在` |
  | `401` | 未登录 | 见上 |
  | `404` | 文章不存在 | `文章不存在` |

- 副作用：写 `posts`；重建 `tags`。幂等。

### DELETE /api/blog/admin/posts/:id

- 鉴权：需要。
- 请求参数：`id`（path，int）。
- curl：`curl -s -X DELETE -H "Authorization: Bearer <jwt>" http://127.0.0.1:19255/api/blog/admin/posts/1`
- 成功响应：`200`，`{"ok":true,"message":"已删除"}`。
- 错误：`401`（未登录）、`404 文章不存在`。
- 副作用：删 `posts` 行；重建 `tags`。**幂等**（重复删除第二次 404）。

## 管理端合集

### GET /api/blog/admin/collections

- 鉴权：需要。
- 请求参数：无。
- curl：`curl -s -H "Authorization: Bearer <jwt>" http://127.0.0.1:19255/api/blog/admin/collections`
- 成功响应：`200`，`{"list":[...]}`，`post_count` 计**全部**文章（含草稿）。
- 错误：`401`（未登录）。
- 副作用：无。幂等。

### POST /api/blog/admin/collections

- 鉴权：需要。
- 请求参数：

  | 名称 | 位置 | 类型 | 必填 | 默认 | 校验 |
  |---|---|---|---|---|---|
  | `name` | body | string | 是 | — | 去首尾空白后不得为空，否则 400 |
  | `slug` | body | string | 否 | 由 `name` 生成 | 冲突加序号 |
  | `description` | body | string | 否 | `""` | 去首尾空白 |

- curl：

  ```bash
  curl -s -X POST -H "Authorization: Bearer <jwt>" -H 'Content-Type: application/json' \
    -d '{"name":"技术","description":"描述"}' \
    http://127.0.0.1:19255/api/blog/admin/collections
  ```

- 成功响应：`201`，`{"id":1,"name":"技术","slug":"技术","public_id":"...","description":"描述","created_at":"..."}`。
- 错误：`400 合集名称不能为空`、`401`。
- 副作用：写 `collections`。非幂等。

### PUT /api/blog/admin/collections/:id

- 鉴权：需要。
- 请求参数：

  | 名称 | 位置 | 类型 | 必填 | 默认 | 校验 |
  |---|---|---|---|---|---|
  | `id` | path | int | 是 | — | 非整数按 `0` |
  | `name` | body | string | 否 | 保持原值 | `null` 保持 |
  | `slug` | body | string | 否 | 保持原值 | 非空才改 |
  | `description` | body | string | 否 | 保持原值 | `null` 保持 |

- curl：

  ```bash
  curl -s -X PUT -H "Authorization: Bearer <jwt>" -H 'Content-Type: application/json' \
    -d '{"name":"新名字"}' \
    http://127.0.0.1:19255/api/blog/admin/collections/1
  ```

- 成功响应：`200`，更新后的合集行。
- 错误：`401`、`404 合集不存在`。
- 副作用：写 `collections`。幂等。

### DELETE /api/blog/admin/collections/:id

- 鉴权：需要。
- 请求参数：`id`（path，int）。
- curl：`curl -s -X DELETE -H "Authorization: Bearer <jwt>" http://127.0.0.1:19255/api/blog/admin/collections/1`
- 成功响应：`200`，`{"ok":true,"message":"已删除"}`。
- 错误：`401`、`404 合集不存在`。
- 副作用：先把关联文章的 `collection_id` 置 `NULL`，再删合集行。**幂等**（重复删除 404）。

### POST /api/blog/admin/upload

- 鉴权：需要。
- 请求参数：

  | 名称 | 位置 | 类型 | 必填 | 默认 | 校验 |
  |---|---|---|---|---|---|
  | `image` | multipart form | file | 是 | — | 扩展名限 `.png .jpg .jpeg .gif .webp .avif .svg`；单文件 ≤ 10MB |

- curl：`curl -s -X POST -H "Authorization: Bearer <jwt>" -F image=@/path/a.png http://127.0.0.1:19255/api/blog/admin/upload`
- 成功响应：`200`，`{"url":"/api/blog/uploads/1792083635184-911162.png"}`。
- 错误：

  | 状态码 | 条件 | message |
  |---|---|---|
  | `400` | 扩展名不在白名单 | `不支持的文件类型 .txt，仅限 .png / .jpg / .jpeg / .gif / .webp / .avif / .svg` |
  | `400` | 字段缺失 | `未收到图片（字段名 image）` |
  | `400` | 文件过大或表单无效 | 见下「已知文案差异」 |
  | `401` | 未登录 | 见上 |

- 副作用：把文件写入 `UPLOAD_DIR`（默认 `./data/uploads`），文件名 `<毫秒>-<0..1000000><扩展名>`。非幂等。
- 备注：超限/表单错误的文案与旧实现 multer 的英文文案不同（旧实现会把 multer 原始英文错误透出），
  属有意为之的中文化差异，状态码一致（`400`）。
