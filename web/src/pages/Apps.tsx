import { useCallback, useEffect, useRef, useState } from 'react'
import { fetchPublicApps, type PublicApp, type PublicAppsPayload } from '../api'
import { STATUS_LABEL, groupApps, latencyText, normalizeStatus } from '../appStatus'
import { LoadingState, ErrorState, EmptyState } from '../components/States'

const REFRESH_MS = 30000

interface AppRowProps {
  /** 单个应用条目，来自公开只读接口 `/api/public/apps`。 */
  app: PublicApp
}

/**
 * 应用列表的一行：应用名（有对外地址时为链接）+ 状态文字 + 可选延迟。
 *
 * 纯文本，不画图标、不用色点表示状态；状态文案由 `appStatus.ts` 给出，这里只做展示。
 * 有 `url` 时新标签打开并带 `rel="noopener noreferrer"`；无 `url` 的基础服务只展示名称。
 */
export function AppRow({ app }: AppRowProps) {
  const status = normalizeStatus(app.status)
  const latency = latencyText(app.latencyMs, app.status)

  return (
    <li>
      {app.url ? (
        <a href={app.url} target="_blank" rel="noopener noreferrer">
          {app.name}
        </a>
      ) : (
        app.name
      )}
      {' · '}
      {STATUS_LABEL[status]}
      {latency ? ` · ${latency}` : null}
      {app.desc ? <p>{app.desc}</p> : null}
    </li>
  )
}

/**
 * 应用中心 `/apps`：按分类分组展示公开应用与实时状态。
 *
 * 数据只来自同源 `GET /api/public/apps`（`cache: 'no-store'`），30s 自动刷新；
 * 页面隐藏时暂停刷新，恢复可见立即刷一次。刷新失败时保留上一次成功的数据，只在顶部提示一行。
 */
export default function Apps() {
  const [data, setData] = useState<PublicAppsPayload | null>(null)
  const [failed, setFailed] = useState(false)
  const [loading, setLoading] = useState(false)
  const aliveRef = useRef(true)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const payload = await fetchPublicApps()
      if (!aliveRef.current) return
      setData(payload && typeof payload === 'object' ? payload : { categories: [], apps: [] })
      setFailed(false)
    } catch {
      // 失败保留上一次成功的数据，只在页面顶部给一行提示
      if (!aliveRef.current) return
      setFailed(true)
    } finally {
      if (aliveRef.current) setLoading(false)
    }
  }, [])

  useEffect(() => {
    aliveRef.current = true
    return () => {
      aliveRef.current = false
    }
  }, [])

  // 30s 自动刷新；页面隐藏时暂停，恢复可见立即刷一次。
  useEffect(() => {
    load()
    const timer = setInterval(() => {
      if (document.visibilityState !== 'hidden') load()
    }, REFRESH_MS)
    const onVisible = () => {
      if (document.visibilityState !== 'hidden') load()
    }
    document.addEventListener('visibilitychange', onVisible)
    return () => {
      clearInterval(timer)
      document.removeEventListener('visibilitychange', onVisible)
    }
  }, [load])

  const apps = Array.isArray(data?.apps) ? data.apps : []
  const categories = Array.isArray(data?.categories) ? data.categories : []
  const groups = groupApps(categories, apps)

  return (
    <section>
      <header>
        <p>Index</p>
        <h1>Apps</h1>
        <p>{data ? `${apps.length} apps` : '—'}</p>
        <button type="button" onClick={load} disabled={loading}>
          刷新
        </button>
      </header>

      {failed && data && <p>暂时无法更新</p>}

      {!data && loading && <LoadingState />}
      {!data && !loading && failed && <ErrorState onRetry={load} />}
      {data && apps.length === 0 && <EmptyState message="暂无应用。" />}

      {groups.map((group) => (
        <section key={group.id}>
          <h2>
            {group.name}（{group.apps.length}）
          </h2>
          <ul>
            {group.apps.map((app) => (
              <AppRow key={app.id} app={app} />
            ))}
          </ul>
        </section>
      ))}
    </section>
  )
}
