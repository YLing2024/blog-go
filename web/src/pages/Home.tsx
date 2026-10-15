import { Link } from 'react-router-dom'
import { fetchPosts, type BlogPostSummary } from '../api'
import { formatDate } from '../utils'
import useAsync from '../hooks/useAsync'
import { LoadingState, ErrorState, EmptyState } from '../components/States'

function Hero() {
  return (
    <section id="top">
      <p>A quiet corner of the web, made by hand</p>
      <h1>Linden Zhang</h1>
      <p>Interfaces on the front. Infrastructure underneath.</p>
      <blockquote>
        <p>立志欲坚不欲锐，成功在久不在速。</p>
        <p>—— 张孝祥</p>
      </blockquote>
    </section>
  )
}

function About() {
  return (
    <section id="about">
      <h2>About</h2>
      <p>I make things for the web — and I run them myself.</p>
      <p>
        Small tools, quiet servers, and no hurry. If the interface disappears and the page simply
        loads, it is enough.
      </p>
    </section>
  )
}

function LatestPosts() {
  const { status, data, retry } = useAsync<BlogPostSummary[]>(fetchPosts, [])
  const latest = (data || []).slice(0, 3)

  return (
    <section id="latest">
      <h2>Writing</h2>
      {status === 'loading' && <LoadingState />}
      {status === 'error' && <ErrorState onRetry={retry} />}
      {status === 'success' && latest.length === 0 && <EmptyState message="No posts yet." />}
      {status === 'success' && latest.length > 0 && (
        <>
          <ul>
            {latest.map((post) => (
              <li key={post.id}>
                <time dateTime={post.created_at || undefined}>{formatDate(post.created_at)}</time>{' '}
                <Link to={`/blog/${post.public_id || post.slug}`}>{post.title}</Link>
                {post.excerpt && <p>{post.excerpt}</p>}
              </li>
            ))}
          </ul>
          <p>
            <Link to="/blog">All posts</Link>
          </p>
        </>
      )}
    </section>
  )
}

function Contact() {
  return (
    <section id="contact">
      <h2>Contact</h2>
      <p>
        Working on something interesting? I&apos;m usually up for a chat about the web,
        self-hosting, or both.
      </p>
      <ul>
        <li>
          <a href="mailto:zhangyunlingzh@gmail.com">zhangyunlingzh@gmail.com</a>
        </li>
        <li>
          <a href="https://github.com/YLing2024" target="_blank" rel="noreferrer">
            GitHub
          </a>
        </li>
      </ul>
    </section>
  )
}

/** 首页：Hero、About、最新三篇文章、Contact，全部为普通 HTML 结构。 */
export default function Home() {
  return (
    <>
      <Hero />
      <About />
      <LatestPosts />
      <Contact />
    </>
  )
}
