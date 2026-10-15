import { Fragment, useEffect, useState } from 'react'
import { Routes, Route, NavLink, Link, Outlet, useLocation } from 'react-router-dom'
import Home from './pages/Home'
import BlogList from './pages/BlogList'
import BlogPost from './pages/BlogPost'
import Collections from './pages/Collections'
import CollectionDetail from './pages/CollectionDetail'
import Apps from './pages/Apps'
import { getTheme, cycleTheme, applyTheme, type Theme } from './theme'

// 主题按钮的可见文案：三个主题各一项，键必须是完整主题集合（缺一个会渲染出洞）。
const THEME_LABEL: Record<Theme, string> = {
  auto: 'Auto',
  light: 'Light',
  dark: 'Dark',
}

// 主题按钮的 title / aria-label：说明当前处于哪一态、点一下会换。
const THEME_TITLE: Record<Theme, string> = {
  auto: 'Theme: system (click to change)',
  light: 'Theme: light (click to change)',
  dark: 'Theme: dark (click to change)',
}

// 顶栏链接。end 只对首页为真，避免 / 在子路径下也判为激活（react-router 的 NavLink 语义）。
const NAV_LINKS = [
  { to: '/', label: 'Home', end: true },
  { to: '/blog', label: 'Blog', end: false },
  { to: '/apps', label: 'Apps', end: false },
] as const

// 路由切换后回到页首。这里刻意不做平滑滚动：滚动动效属于自定义视觉，已随朴素排版删除。
function ScrollToTop() {
  const { pathname } = useLocation()

  useEffect(() => {
    window.scrollTo(0, 0)
  }, [pathname])

  return null
}

// 顶栏：普通 <nav> + 行内链接，任何视口都可见；窄屏由浏览器自动换行，没有抽屉与触发按钮。
function Nav() {
  const [theme, setTheme] = useState<Theme>(() => getTheme())

  useEffect(() => {
    applyTheme(theme)
  }, [theme])

  // 跟随系统时，系统配色变化要实时反映到页面上（data-theme 不落地，靠 color-scheme 自动跟随）。
  useEffect(() => {
    if (theme !== 'auto') return
    if (typeof window === 'undefined' || !window.matchMedia) return
    const mq = window.matchMedia('(prefers-color-scheme: dark)')
    const onChange = () => applyTheme('auto')
    mq.addEventListener('change', onChange)
    return () => mq.removeEventListener('change', onChange)
  }, [theme])

  return (
    <header>
      <nav className="container" aria-label="Primary">
        <Link to="/" aria-label="Linden Zhang — Home">
          ◇
        </Link>{' '}
        {NAV_LINKS.map(({ to, label, end }) => (
          <Fragment key={to}>
            <NavLink to={to} end={end}>
              {label}
            </NavLink>{' '}
          </Fragment>
        ))}
        <button
          type="button"
          onClick={() => setTheme(cycleTheme())}
          title={THEME_TITLE[theme]}
          aria-label={THEME_TITLE[theme]}
        >
          {THEME_LABEL[theme]}
        </button>
      </nav>
    </header>
  )
}

function Footer() {
  return (
    <footer className="container">
      <p>© 2026 Linden Zhang</p>
      <p>Made by hand</p>
    </footer>
  )
}

function Layout() {
  return (
    <>
      <ScrollToTop />
      <Nav />
      <main className="container">
        <Outlet />
      </main>
      <Footer />
    </>
  )
}

/** 应用外壳：顶栏（普通 nav）、主内容容器、纯文本页脚，外加路由表。 */
export default function App() {
  return (
    <Routes>
      <Route element={<Layout />}>
        <Route index element={<Home />} />
        <Route path="apps" element={<Apps />} />
        <Route path="blog" element={<BlogList />} />
        <Route path="blog/collections" element={<Collections />} />
        <Route path="blog/collections/:slug" element={<CollectionDetail />} />
        <Route path="blog/:slug" element={<BlogPost />} />
      </Route>
    </Routes>
  )
}
