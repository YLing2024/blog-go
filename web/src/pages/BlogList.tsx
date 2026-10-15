import { useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { fetchPosts, type BlogPostSummary } from '../api'
import { formatDate } from '../utils'
import useAsync from '../hooks/useAsync'
import { LoadingState, ErrorState, EmptyState } from '../components/States'

// 标签数组的保守收窄：后端字段可缺省，缺省时按空数组处理
function postTags(post: { tags?: string[] | null }): string[] {
  return post.tags || []
}

/** 博客列表 `/blog`：按标题 / 摘要 / 标签在前端本地过滤；链接优先 `public_id`、回退 `slug`。 */
export default function BlogList() {
  const { status, data, retry } = useAsync<BlogPostSummary[]>(fetchPosts, [])
  const [query, setQuery] = useState<string>('')

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return data || []
    return (data || []).filter((post) => {
      const title = (post.title || '').toLowerCase()
      const excerpt = (post.excerpt || '').toLowerCase()
      const tags = (post.tags || []).join(' ').toLowerCase()
      return title.includes(q) || excerpt.includes(q) || tags.includes(q)
    })
  }, [data, query])

  return (
    <section>
      <header>
        <p>Blog</p>
        <h1>Writing</h1>
        <p>
          <input
            type="search"
            placeholder="Search by title, excerpt or tag…"
            aria-label="Search articles"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
          />
        </p>
        <p>{status === 'success' && data ? `${data.length} posts` : '—'}</p>
        <p>
          <Link to="/blog/collections">Collections</Link>
        </p>
      </header>

      {status === 'loading' && <LoadingState />}
      {status === 'error' && <ErrorState onRetry={retry} />}
      {status === 'success' && data && data.length === 0 && <EmptyState message="No posts yet." />}
      {status === 'success' && data && data.length > 0 && filtered.length === 0 && (
        <EmptyState message="Nothing matches your search." />
      )}

      {status === 'success' && filtered.length > 0 && (
        <ul>
          {filtered.map((post) => (
            <li key={post.id}>
              <article>
                <time dateTime={post.created_at || undefined}>{formatDate(post.created_at)}</time>
                <h2>
                  <Link to={`/blog/${post.public_id || post.slug}`}>{post.title}</Link>
                </h2>
                {post.excerpt && <p>{post.excerpt}</p>}
                {(post.collection || postTags(post).length > 0) && (
                  <p>
                    {post.collection && (
                      <Link
                        to={`/blog/collections/${post.collection.public_id || post.collection.slug}`}
                      >
                        {post.collection.name}
                      </Link>
                    )}
                    {post.collection && postTags(post).length > 0 ? ' · ' : null}
                    {postTags(post).join(' · ')}
                  </p>
                )}
              </article>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
