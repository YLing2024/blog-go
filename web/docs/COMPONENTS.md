# 组件登记表（COMPONENTS.md）

本仓库全部可复用组件的登记处。新增或改动可复用组件时，**同笔**更新本表，并在组件文件里补 TSDoc。

约定：

- 组件只负责结构与语义，**不写样式**（颜色 / 圆角 / 字号一律不出现在组件层）。
- 不引入 Storybook；示例就是可复制粘贴的最小用法。
- 组件文件头写 TSDoc：用途、props（含类型与默认值）、可访问性要点（键盘 / aria / 焦点）。

| 组件 | 用途 | 关键 props | 最小示例 | 注意事项 |
|---|---|---|---|---|
| `LoadingState` | 加载中占位 | `label?: string`（默认 `Loading…`） | `<LoadingState />` | `role="status"`，温和播报；不要塞长文案 |
| `ErrorState` | 加载失败占位 | `onRetry?: () => void`（可选） | `<ErrorState onRetry={retry} />` | `role="alert"`，会立即播报；不传 `onRetry` 时不显示重试按钮 |
| `EmptyState` | 空数据占位 | `message?: string`（默认 `Nothing here yet.`） | `<EmptyState message="No posts yet." />` | 纯文本；文案由调用方给，避免在此处硬编码业务文案 |
| `AppRow` | 应用中心列表的一行 | `app: PublicApp`（必填） | `<ul>{apps.map((a) => <AppRow key={a.id} app={a} />)}</ul>` | 必须放在 `<ul>` 内（渲染 `<li>`）；`app.url` 存在时渲染新标签链接并带 `rel="noopener noreferrer"`；状态只输出文字，无图标、无色点 |

## 相关 hook

| Hook | 用途 | 关键入参 | 最小示例 | 注意事项 |
|---|---|---|---|---|
| `useAsync` | 统一的加载 / 成功 / 失败三态 | `fn: () => Promise<T>`、`deps: unknown[]` | `const { status, data, retry } = useAsync(fetchPosts, [])` | 每个页面各自调用，不进 `<Suspense>`；`retry` 用于失败重试 |
