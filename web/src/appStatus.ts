import type { AppStatus, PublicApp, PublicAppCategory } from './api'

// 状态词（status → 展示文案）；口径与登记侧一致，不在此处自创词。
export const STATUS_LABEL: Record<AppStatus, string> = {
  up: '正常',
  auth: '需登录',
  degraded: '响应慢',
  down: '异常',
  idle: '休眠',
  unknown: '未知',
}

// 接口 status → 白名单内状态；未登记 / 缺失一律归为 unknown。
export function normalizeStatus(status: string | null | undefined): AppStatus {
  return status && status in STATUS_LABEL ? (status as AppStatus) : 'unknown'
}

// 状态 → 展示文案。
export function statusLabel(status: string | null | undefined): string {
  return STATUS_LABEL[normalizeStatus(status)]
}

// 异常口径：只有连不上（down）与响应异常（degraded）算异常；
// idle 是按需唤醒应用的休眠态，不计入异常。
export function isAbnormal(status: string | null | undefined): boolean {
  const normalized = normalizeStatus(status)
  return normalized === 'down' || normalized === 'degraded'
}

// 延迟文案：idle 不显示延迟，数值缺失 / 非有限数也不显示；其余形如 "147 ms"。
export function latencyText(
  latencyMs: number | null | undefined,
  status: string | null | undefined,
): string | null {
  if (normalizeStatus(status) === 'idle') return null
  if (latencyMs == null) return null
  const value = Number(latencyMs)
  if (!Number.isFinite(value)) return null
  return `${Math.round(value)} ms`
}

export interface AppGroup {
  id: string
  name: string
  apps: PublicApp[]
}

const OTHER_NAME = '其他'

// 按 categories 的原始顺序分组；组内保持 apps 原顺序；空组不返回。
// 未匹配到任何分类的应用归入「其他」：若 categories 已含 other 组则并入该组，
// 否则追加一个合成的「其他」组。
export function groupApps(
  categories: PublicAppCategory[],
  apps: PublicApp[],
): AppGroup[] {
  const known = new Set(categories.map((category) => category.id))
  const hasOther = known.has('other')

  const keyOf = (app: PublicApp): string =>
    known.has(app.category) ? app.category : hasOther ? 'other' : '__other'

  const groups: AppGroup[] = categories.map((category) => ({
    id: category.id,
    name: category.name,
    apps: apps.filter((app) => keyOf(app) === category.id),
  }))

  if (!hasOther) {
    const rest = apps.filter((app) => keyOf(app) === '__other')
    if (rest.length) groups.push({ id: '__other', name: OTHER_NAME, apps: rest })
  }

  return groups.filter((group) => group.apps.length > 0)
}
