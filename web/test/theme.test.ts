import test from 'node:test'
import assert from 'node:assert/strict'
import { getTheme, nextTheme } from '../src/theme.ts'

// 用一个可替换的 localStorage 假件覆盖全局，跑完还原。
// 不假设 node 是否内置 localStorage，因此用 defineProperty 直接换掉。
function withLocalStorage<T>(
  fake: { getItem: (key: string) => string | null },
  run: () => T,
): T {
  const original = Object.getOwnPropertyDescriptor(globalThis, 'localStorage')
  Object.defineProperty(globalThis, 'localStorage', {
    configurable: true,
    writable: true,
    value: fake,
  })
  try {
    return run()
  } finally {
    if (original) Object.defineProperty(globalThis, 'localStorage', original)
    else delete (globalThis as { localStorage?: unknown }).localStorage
  }
}

test('nextTheme：auto → light → dark → auto 循环', () => {
  assert.equal(nextTheme('auto'), 'light')
  assert.equal(nextTheme('light'), 'dark')
  assert.equal(nextTheme('dark'), 'auto')
  // 走满一圈回到原点
  assert.equal(nextTheme(nextTheme(nextTheme('auto'))), 'auto')
})

test('getTheme：合法值原样返回；缺失 / 非法 / 存储抛错时回退 auto', () => {
  const stored = new Map<string, string>()
  withLocalStorage({ getItem: (key) => stored.get(key) ?? null }, () => {
    stored.set('site_theme', 'dark')
    assert.equal(getTheme(), 'dark')

    stored.set('site_theme', 'nope')
    assert.equal(getTheme(), 'auto')

    stored.delete('site_theme')
    assert.equal(getTheme(), 'auto')
  })

  // 隐私模式下 localStorage 抛错：必须静默回退，不能把异常抛给调用方
  withLocalStorage(
    {
      getItem: () => {
        throw new Error('storage blocked')
      },
    },
    () => {
      assert.equal(getTheme(), 'auto')
    },
  )
})
