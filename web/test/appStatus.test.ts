import test from 'node:test'
import assert from 'node:assert/strict'
import {
  STATUS_LABEL,
  groupApps,
  isAbnormal,
  latencyText,
  normalizeStatus,
  statusLabel,
} from '../src/appStatus.ts'

// 构造最小应用对象；只填分组/状态相关的字段。
function app(id, category, extra = {}) {
  return { id, name: id, category, ...extra }
}

const CATEGORIES = [
  { id: 'self', name: '自研' },
  { id: 'third', name: '第三方' },
  { id: 'infra', name: '基础' },
  { id: 'other', name: '其他' },
]

test('groupApps：按 categories 原始顺序分组，空组不返回', () => {
  const groups = groupApps(CATEGORIES, [
    app('hub', 'third'),
    app('homepage', 'self'),
    app('keep', 'infra'),
  ])

  assert.deepEqual(
    groups.map((group) => group.id),
    ['self', 'third', 'infra'],
  )
  assert.deepEqual(groups.map((group) => group.name), ['自研', '第三方', '基础'])
  assert.deepEqual(
    groups.map((group) => group.apps.map((item) => item.id)),
    [['homepage'], ['hub'], ['keep']],
  )
})

test('groupApps：组内保持 apps 原顺序', () => {
  const groups = groupApps(CATEGORIES, [
    app('b', 'self'),
    app('a', 'self'),
    app('c', 'self'),
  ])

  assert.deepEqual(groups[0].apps.map((item) => item.id), ['b', 'a', 'c'])
})

test('groupApps：未知分类归入「其他」（categories 已含 other 时并入该组）', () => {
  const groups = groupApps(CATEGORIES, [
    app('known', 'self'),
    app('stray', 'nope'),
    app('plain', 'other'),
  ])

  const other = groups.find((group) => group.id === 'other')
  assert.ok(other)
  assert.equal(other.name, '其他')
  assert.deepEqual(other.apps.map((item) => item.id), ['stray', 'plain'])
  // 没有生成第二个「其他」组
  assert.equal(groups.filter((group) => group.name === '其他').length, 1)
})

test('groupApps：categories 不含 other 时，未知分类追加合成的「其他」组', () => {
  const plain = CATEGORIES.slice(0, 3)
  const groups = groupApps(plain, [app('known', 'infra'), app('stray', 'nope')])

  assert.deepEqual(
    groups.map((group) => group.id),
    ['infra', '__other'],
  )
  assert.equal(groups[1].name, '其他')
  assert.deepEqual(groups[1].apps.map((item) => item.id), ['stray'])
})

test('groupApps：无应用时返回空数组', () => {
  assert.deepEqual(groupApps(CATEGORIES, []), [])
  assert.deepEqual(groupApps([], [app('solo', 'x')]), [
    { id: '__other', name: '其他', apps: [app('solo', 'x')] },
  ])
})

test('STATUS_LABEL / statusLabel：状态文案映射，未登记状态归「未知」', () => {
  assert.deepEqual(STATUS_LABEL, {
    up: '正常',
    auth: '需登录',
    degraded: '响应慢',
    down: '异常',
    idle: '休眠',
    unknown: '未知',
  })

  assert.equal(statusLabel('up'), '正常')
  assert.equal(statusLabel('auth'), '需登录')
  assert.equal(statusLabel('degraded'), '响应慢')
  assert.equal(statusLabel('down'), '异常')
  assert.equal(statusLabel('idle'), '休眠')
  assert.equal(statusLabel('unknown'), '未知')

  assert.equal(normalizeStatus('nope'), 'unknown')
  assert.equal(statusLabel('nope'), '未知')
  assert.equal(statusLabel(null), '未知')
  assert.equal(statusLabel(undefined), '未知')
  assert.equal(statusLabel(''), '未知')
})

test('isAbnormal：idle 不计入异常，只有 down / degraded 算异常', () => {
  assert.equal(isAbnormal('down'), true)
  assert.equal(isAbnormal('degraded'), true)

  assert.equal(isAbnormal('idle'), false)
  assert.equal(isAbnormal('up'), false)
  assert.equal(isAbnormal('auth'), false)
  assert.equal(isAbnormal('unknown'), false)
  assert.equal(isAbnormal(null), false)
})

test('latencyText：数值形如 "147 ms"，null / 非有限数不显示', () => {
  assert.equal(latencyText(147, 'up'), '147 ms')
  assert.equal(latencyText(147.6, 'up'), '148 ms')
  assert.equal(latencyText(0, 'up'), '0 ms')

  assert.equal(latencyText(null, 'up'), null)
  assert.equal(latencyText(undefined, 'up'), null)
  assert.equal(latencyText(Number.NaN, 'up'), null)
  assert.equal(latencyText(Number.POSITIVE_INFINITY, 'up'), null)
})

test('latencyText：idle 一律不显示延迟，即使带数值', () => {
  assert.equal(latencyText(147, 'idle'), null)
  assert.equal(latencyText(0, 'idle'), null)
  assert.equal(latencyText(null, 'idle'), null)
})
