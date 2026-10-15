import { Link } from 'react-router-dom'
import { getCollections, type BlogCollection } from '../api'
import useAsync from '../hooks/useAsync'
import { LoadingState, ErrorState, EmptyState } from '../components/States'

/** 合集列表 `/blog/collections`。 */
export default function Collections() {
  const { status, data, retry } = useAsync<BlogCollection[]>(getCollections, [])

  return (
    <section>
      <header>
        <p>Blog</p>
        <h1>Collections</h1>
        <p>{status === 'success' && data ? `${data.length} collections` : '—'}</p>
      </header>

      {status === 'loading' && <LoadingState />}
      {status === 'error' && <ErrorState onRetry={retry} />}
      {status === 'success' && data && data.length === 0 && (
        <EmptyState message="No collections yet." />
      )}

      {status === 'success' && data && data.length > 0 && (
        <ul>
          {data.map((collection) => (
            <li key={collection.id}>
              <h2>
                <Link to={`/blog/collections/${collection.public_id || collection.slug}`}>
                  {collection.name}
                </Link>
              </h2>
              {collection.description && <p>{collection.description}</p>}
              <p>{collection.post_count} posts</p>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
