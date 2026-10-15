package posts

import (
	"context"
	"database/sql"

	"blog-go/internal/store"
)

// samplePost 是首启示例文章（内容逐字对齐旧实现 db.ts 的 SAMPLE_POSTS）。
type samplePost struct {
	title   string
	slug    string
	excerpt string
	tags    string
	content string
}

// samplePosts 与旧实现完全一致；正文用 ## / ### 分级，绝无一级标题。
var samplePosts = []samplePost{
	{
		title:   "你好，世界：博客正式上线",
		slug:    "hello-world",
		excerpt: "这是博客的第一篇文章，记录这个站点搭建的初衷、设计理念与使用方式。",
		tags:    "随笔, 博客",
		content: "## 欢迎来到我的博客\n\n这里是博客的第一篇文章。从今天开始，我会在这里记录技术笔记、读书心得与生活随笔。\n\n### 为什么搭建这个博客\n\n我希望有一个完全属于自己的空间：没有广告、没有推荐算法，只有文字本身。\n\n### 这个站点的设计理念\n\n- 极简、安静、干净\n- 黑白灰为主，少量琥珀色点缀\n- 不加载任何 Web 字体，保持加载速度\n- 深色模式跟随系统自动切换\n\n一段示例代码：\n\n```js\nconsole.log('Hello, World!')\n```\n\n> 少即是多。\n>\n> —— 好的工具应该让人专注于内容本身。",
	},
	{
		title:   "为什么用 Markdown 写博客",
		slug:    "why-markdown",
		excerpt: "正文与样式分离、对版本控制友好、让人专注写作。聊聊我选择 Markdown 的几个理由。",
		tags:    "Markdown, 写作",
		content: "## 为什么用 Markdown 写博客\n\n在尝试过各种富文本编辑器之后，我最终选择了 Markdown。原因很简单：\n\n### 1. 专注内容\n\nMarkdown 语法足够轻量，写作时不用关心排版，眼睛只盯着文字本身。\n\n### 2. 纯文本，可版本化\n\nMarkdown 文件就是纯文本，可以直接放进 Git 仓库，每一次修改都有记录。\n\n### 3. 随处可用\n\n支持 Markdown 的工具无处不在，写好的内容可以轻易迁移到别的平台。\n\n### 常用语法速览\n\n| 语法        | 效果        |\n| ----------- | ----------- |\n| `**加粗**` | **加粗**    |\n| `*斜体*`   | *斜体*      |\n| `[链接](url)` | [链接](url) |\n\n> 写作的本质是思考，工具越简单越好。",
	},
	{
		title:   "前端性能优化笔记",
		slug:    "frontend-performance-notes",
		excerpt: "从网络、渲染与代码三个层面，整理常见的前端性能优化手段，供日常开发查阅。",
		tags:    "前端, 性能优化",
		content: "## 前端性能优化笔记\n\n性能优化的本质是**减少关键路径上的工作量**。下面按三个层面整理常用的手段。\n\n### 网络层面\n\n- 启用 HTTP/2 与 CDN，减少往返时延\n- 静态资源开启 Gzip / Brotli 压缩\n- 图片使用 WebP / AVIF 格式并懒加载\n- 合理设置缓存策略\n\n```js\n// 懒加载图片的简单写法\nconst img = document.querySelector('img[data-src]')\nimg.addEventListener('load', () => {\n  img.src = img.dataset.src\n})\n```\n\n### 渲染层面\n\n- 避免阻塞渲染的脚本，使用 `defer` / `async`\n- 用 CSS 动画替代 JS 动画\n- 减少重排与重绘，合理使用 `will-change`\n\n### 代码层面\n\n- 组件与路由按需加载\n- 长列表使用虚拟滚动\n- 缓存计算结果，避免重复运算\n\n> 优化要基于数据，先测量再动手。",
	},
}

// Seed 在 posts 表为空时插入 3 篇示例文章（幂等）。返回是否实际写入。
func Seed(ctx context.Context, db *store.DB) (bool, error) {
	var n int64
	if err := db.SQL.QueryRowContext(ctx, "SELECT COUNT(*) FROM posts").Scan(&n); err != nil {
		return false, err
	}
	if n > 0 {
		return false, nil
	}
	err := db.Tx(ctx, func(q store.Querier) error {
		for _, p := range samplePosts {
			now := store.NowText()
			if _, err := q.ExecContext(ctx,
				`INSERT INTO posts (title, slug, content, excerpt, tags, published, created_at, updated_at)
				 VALUES (?, ?, ?, ?, ?, 1, ?, ?)`,
				p.title, p.slug, p.content, p.excerpt, p.tags, now, now); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return true, nil
}

// CountPosts 返回 posts 表条数（验收对拍用）。
func CountPosts(ctx context.Context, db *sql.DB) (int64, error) {
	var n int64
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM posts").Scan(&n)
	return n, err
}
