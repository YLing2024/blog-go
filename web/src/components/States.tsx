/**
 * 异步加载的三种占位态，全站共用。
 *
 * 只输出语义化文本与原生按钮，不携带任何样式（视觉已交回浏览器默认）。
 * 可访问性：loading 用 `role="status"` 温和播报，error 用 `role="alert"` 立即播报；
 * 重试按钮是原生 `<button>`，可键盘触发并保留默认焦点环。
 */

export interface LoadingStateProps {
  /** 占位文案，默认 `Loading…`。 */
  label?: string
}

/** 加载中占位（`role="status"`）。 */
export function LoadingState({ label = 'Loading…' }: LoadingStateProps) {
  return <p role="status">{label}</p>
}

export interface ErrorStateProps {
  /** 提供后渲染「重试」按钮；不提供则只显示错误文案。 */
  onRetry?: () => void
}

/** 加载失败占位（`role="alert"`）；带 `onRetry` 时给出重试入口。 */
export function ErrorState({ onRetry }: ErrorStateProps) {
  return (
    <div role="alert">
      <p>Failed to load. Check your connection and try again.</p>
      {onRetry && (
        <button type="button" onClick={onRetry}>
          Retry
        </button>
      )}
    </div>
  )
}

export interface EmptyStateProps {
  /** 空态文案，默认 `Nothing here yet.`。 */
  message?: string
}

/** 空数据占位。 */
export function EmptyState({ message = 'Nothing here yet.' }: EmptyStateProps) {
  return <p>{message}</p>
}
