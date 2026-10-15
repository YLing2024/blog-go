import { Link, useParams } from 'react-router-dom'
import { getCollection, type BlogCollectionDetail } from '../api'
import { formatDate } from '../utils'
import useAsync from '../hooks/useAsync'
import { LoadingState, ErrorState, EmptyState } from '../components/States'

// 标签数组的保守收窄：后端字段可缺省，缺省时按空数组处理
function postTags(post: { tags?: string[] | null }): string[] {
  return post.tags || []
}

/** 合集详情 `/blog/collections/:slug`：合集说明与其下文章列表。 */
export default function CollectionDetail() {
  const { slug } = useParams<{ slug: string }>()
  const { status, data: collection, retry } = useAsync<BlogCollectionDetail | null>(
    () => getCollection(slug as string),
    [slug],
  )

  return (
    <article>
      {status === 'loading' && <LoadingState />}
      {status === 'error' && <ErrorState onRetry={retry} />}
      {status === 'success' && collection === null && <EmptyState message="Collection not found." />}

      {status === 'success' && collection && (
        <>
          <p>
            <Link to="/blog/collections">← All collections</Link>
          </p>
          <header>
            <p>Collection</p>
            <h1>{collection.name}</h1>
            {collection.description && <p>{collection.description}</p>}
          </header>

          {collection.posts.length === 0 && <EmptyState message="No posts in this collection yet." />}

          {collection.posts.length > 0 && (
            <ul>
              {collection.posts.map((post) => (
                <li key={post.id}>
                  <time dateTime={post.created_at || undefined}>{formatDate(post.created_at)}</time>
                  <h2>
                    <Link to={`/blog/${post.public_id || post.slug}`}>{post.title}</Link>
                  </h2>
                  {post.excerpt && <p>{post.excerpt}</p>}
                  {postTags(post).length > 0 && <p>{postTags(post).join(' · ')}</p>}
                </li>
              ))}
            </ul>
          )}
        </>
      )}
    </article>
  )
}
