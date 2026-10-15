import { useEffect, useMemo, useRef, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import hljs from 'highlight.js/lib/common'
// `common` is the trimmed bundle; these four languages are not in it but ship
// as single files in the same package (no new dependency, no full `highlight.js`).
import dart from 'highlight.js/lib/languages/dart'
import dockerfile from 'highlight.js/lib/languages/dockerfile'
import nginx from 'highlight.js/lib/languages/nginx'
import protobuf from 'highlight.js/lib/languages/protobuf'
import { fetchPost, type BlogPostDetail } from '../api'
import { formatDate, headingId } from '../utils'
import useAsync from '../hooks/useAsync'
import { LoadingState, ErrorState, EmptyState } from '../components/States'

hljs.registerLanguage('dart', dart)
hljs.registerLanguage('dockerfile', dockerfile)
hljs.registerLanguage('nginx', nginx)
hljs.registerLanguage('protobuf', protobuf)

async function codeText(code: HTMLElement | null): Promise<string> {
  const tbody = code?.querySelector('tbody')
  if (!tbody) return code?.textContent || ''
  const lines: string[] = []
  tbody.querySelectorAll('tr').forEach((row) => {
    const td = row.querySelector('.hljs-ln-code')
    if (td) lines.push((td.textContent || '').replace(/\n+$/, ''))
  })
  return lines.join('\n')
}

async function copyText(text: string): Promise<boolean> {
  if (navigator.clipboard && typeof navigator.clipboard.writeText === 'function') {
    try {
      await navigator.clipboard.writeText(text)
      return true
    } catch {
      /* fall through to fallback */
    }
  }
  try {
    const textarea = document.createElement('textarea')
    textarea.value = text
    textarea.style.position = 'fixed'
    textarea.style.opacity = '0'
    document.body.appendChild(textarea)
    textarea.select()
    const ok = document.execCommand('copy')
    document.body.removeChild(textarea)
    return ok
  } catch {
    return false
  }
}

interface TocItem {
  id: string
  text: string
}

/**
 * 文章详情页。
 *
 * Markdown 一律经 `marked` → DOMPurify 消毒后再注入；正文里的 `h1` 统一降级为 `h2`，
 * 保证一页只有一个 `h1`（即标题区）。目录是普通链接列表，锚点用纯序号 `sec-N`，
 * 不做粘性定位与滚动高亮。
 */
export default function BlogPost() {
  const { slug } = useParams<{ slug: string }>()
  const bodyRef = useRef<HTMLDivElement>(null)
  const { status, data: post, retry } = useAsync<BlogPostDetail | null>(
    () => fetchPost(slug as string),
    [slug],
  )
  const [toc, setToc] = useState<TocItem[]>([])

  // Title lives in its own region (an <h1> in the header). The body must never
  // contribute a second h1: any h1 in the markdown is demoted to h2.
  const bodyHtml = useMemo<string>(() => {
    if (!post?.content) return ''
    const raw = marked.parse(post.content)
    const clean = DOMPurify.sanitize(typeof raw === 'string' ? raw : '')
    const doc = new DOMParser().parseFromString(`<div id="post-root">${clean}</div>`, 'text/html')
    const root = doc.getElementById('post-root')!
    root.querySelectorAll('h1').forEach((h) => {
      const h2 = doc.createElement('h2')
      h2.innerHTML = h.innerHTML
      h.replaceWith(h2)
    })
    return root.innerHTML
  }, [post])

  useEffect(() => {
    const body = bodyRef.current
    if (!body) return
    body.querySelectorAll('pre code').forEach((block) => {
      const lang = (block.className.match(/language-([\w-]+)/) || [])[1]
      // No explicit language → leave it as plain text. Auto-detect mangles
      // mixed CJK snippets and writes a bogus `language-undefined` class.
      if (!lang) return
      const el = block as HTMLElement
      el.dataset.lang = lang
      hljs.highlightElement(el)
    })

    // Copy button per code block. No positioning: it simply follows the block in
    // normal flow, because absolute placement was part of the removed styling.
    body.querySelectorAll('pre').forEach((pre) => {
      const code = pre.querySelector('code')
      const button = document.createElement('button')
      button.type = 'button'
      button.textContent = 'Copy'
      button.setAttribute('aria-label', 'Copy code')
      button.addEventListener('click', async () => {
        const ok = await copyText(await codeText(code))
        button.textContent = ok ? 'Copied' : 'Copy failed'
        window.setTimeout(() => {
          button.textContent = 'Copy'
        }, 1600)
      })
      pre.insertAdjacentElement('afterend', button)
    })

    let alive = true
    window.hljs = hljs
    import('highlightjs-line-numbers.js').then(() => {
      if (!alive || !window.hljs?.lineNumbersBlockSync) return
      body.querySelectorAll('pre > code').forEach((block) => {
        if (block.querySelector('.hljs-ln')) return
        if (!block.textContent!.trim() && !(block as HTMLElement).dataset.lang) return
        window.hljs!.lineNumbersBlockSync!(block as HTMLElement, { singleLine: true })
      })
    })
    return () => {
      alive = false
    }
  }, [bodyHtml])

  // Collect the table of contents: anchor every h1/h2/h3, keep their text.
  // h1 is included on purpose: articles written with `#` sections (older posts)
  // would otherwise show up with a nearly empty Contents list.
  useEffect(() => {
    const body = bodyRef.current
    if (!body) return
    const headings = [...body.querySelectorAll('h1, h2, h3')]
    headings.forEach((h, i) => {
      if (!h.id) h.id = headingId(i)
    })
    setToc(headings.map((h) => ({ id: h.id, text: h.textContent!.trim() })))
  }, [bodyHtml])

  return (
    <article>
      {status === 'loading' && <LoadingState />}
      {status === 'error' && <ErrorState onRetry={retry} />}
      {status === 'success' && post === null && (
        <EmptyState message="Post not found or unpublished." />
      )}

      {status === 'success' && post && (
        <>
          <p>
            <Link to="/blog">← All posts</Link>
          </p>
          <header>
            <p>{post.published === false ? 'Draft preview' : 'Article'}</p>
            <h1>{post.title}</h1>
            {post.subtitle ? <p className="post__subtitle">{post.subtitle}</p> : null}
            <p>
              <time dateTime={post.created_at || undefined}>{formatDate(post.created_at)}</time>
              {post.collection && (
                <>
                  {' · '}
                  <Link
                    to={`/blog/collections/${post.collection.public_id || post.collection.slug}`}
                  >
                    {post.collection.name}
                  </Link>
                </>
              )}
              {post.tags && post.tags.length > 0 ? ` · ${post.tags.join(' · ')}` : null}
            </p>
          </header>

          {toc.length > 0 && (
            <nav aria-label="Contents">
              <ul>
                {toc.map((item) => (
                  <li key={item.id}>
                    <a href={`#${item.id}`}>{item.text}</a>
                  </li>
                ))}
              </ul>
            </nav>
          )}

          {bodyHtml && (
            <div
              ref={bodyRef}
              className="post__body"
              dangerouslySetInnerHTML={{ __html: bodyHtml }}
            />
          )}
        </>
      )}
    </article>
  )
}
