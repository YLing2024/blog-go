const THEME_KEY = 'site_theme'

// 主题的三个取值 —— 同时作为运行时白名单与字面量联合类型的唯一来源
const THEMES = ['auto', 'light', 'dark'] as const
export type Theme = (typeof THEMES)[number]

function isTheme(value: string | null): value is Theme {
  return value !== null && (THEMES as readonly string[]).includes(value)
}

/** 读取持久化的主题；缺省 / 非法 / 存储不可用时回退 `auto`。 */
export function getTheme(): Theme {
  try {
    const stored = localStorage.getItem(THEME_KEY)
    return isTheme(stored) ? stored : 'auto'
  } catch {
    // localStorage 不可用（隐私模式）时按「跟随系统」处理
    return 'auto'
  }
}

// 三态循环顺序的唯一来源；抽成纯映射便于单测。
const NEXT_THEME: Record<Theme, Theme> = { auto: 'light', light: 'dark', dark: 'auto' }

/** 三态循环：auto → light → dark → auto。 */
export function nextTheme(theme: Theme): Theme {
  return NEXT_THEME[theme]
}

/**
 * 把主题落到 `<html data-theme>`（`auto` 时清除该属性，交给 `color-scheme` 自动跟随），
 * 并开合代码高亮的两份 hljs 皮肤 link。
 *
 * 为什么还要动 hljs link：两份皮肤靠 `prefers-color-scheme` 切换，而手动选择只改
 * `color-scheme`、不会改 `prefers-color-scheme`，不换 `media` 的话深色下代码块仍是浅色的。
 */
export function applyTheme(theme: Theme): void {
  const root = document.documentElement

  if (theme === 'auto') {
    delete root.dataset.theme
  } else {
    root.dataset.theme = theme
  }

  try {
    localStorage.setItem(THEME_KEY, theme)
  } catch {
    // localStorage 不可用（隐私模式）时静默降级
  }

  const lightLink = document.getElementById('hljs-light')
  const darkLink = document.getElementById('hljs-dark')

  if (theme === 'auto') {
    lightLink?.setAttribute('media', '(prefers-color-scheme: light)')
    darkLink?.setAttribute('media', '(prefers-color-scheme: dark)')
  } else if (theme === 'dark') {
    lightLink?.setAttribute('media', 'not all')
    darkLink?.setAttribute('media', 'all')
  } else {
    lightLink?.setAttribute('media', 'all')
    darkLink?.setAttribute('media', 'not all')
  }
}

/** 切到下一态、立即应用，并返回新主题。 */
export function cycleTheme(): Theme {
  const next = nextTheme(getTheme())
  applyTheme(next)
  return next
}
