// Service worker for the static homepage SPA.
//
// Hand-written single file, no build step and no workbox. Registered from
// src/pwa.ts (production builds only).
//
// Caching is whitelist-only: only the request classes listed under ALLOWED
// below are cached. Everything else — admin APIs, the auth gateway, temporary
// links, non-GET requests, cross-origin requests and draft-preview URLs — is
// passed straight through to the network and never written to a cache.
//
// Update reliability (top priority): a fresh build must reach users on the
// next load. install() calls skipWaiting() and activate() calls clients.claim(),
// so the new worker takes over immediately; the page side reloads once when
// control changes (see src/pwa.ts).

// Single version constant. Bump it on a release that needs to invalidate
// cached responses; activate() then drops every cache that is not this one.
const VERSION = 'v2'
const CACHE_PREFIX = 'homepage-'
const CACHE_NAME = `${CACHE_PREFIX}${VERSION}`

// Same-origin URLs precached at install time so the offline fallback chain
// always has something to serve: the SPA shell and the offline page.
const PRECACHE_URLS = ['/index.html', '/offline.html']

// Request classes allowed to be cached (section A of the spec).
const STATIC_PREFIXES = ['/hljs/', '/icons/']
const IMAGE_EXT = /\.(?:png|jpe?g|webp|svg|ico)$/i

// ---------------------------------------------------------------------------
// install / activate
// ---------------------------------------------------------------------------

self.addEventListener('install', (event) => {
  event.waitUntil(
    caches
      .open(CACHE_NAME)
      .then((cache) => cache.addAll(PRECACHE_URLS))
      // Take over right away instead of waiting for every tab to close, so a
      // rebuilt deployment is picked up on the next load.
      .then(() => self.skipWaiting()),
  )
})

self.addEventListener('activate', (event) => {
  event.waitUntil(
    (async () => {
      // Drop every cache that is not the current version. This also clears
      // caches left behind by the old root-path worker this site used to have.
      const keys = await caches.keys()
      await Promise.all(
        keys.filter((key) => key !== CACHE_NAME).map((key) => caches.delete(key)),
      )
      // Start controlling open pages immediately so the update is not stuck
      // behind a reload.
      await self.clients.claim()
    })(),
  )
})

// ---------------------------------------------------------------------------
// fetch routing
// ---------------------------------------------------------------------------

self.addEventListener('fetch', (event) => {
  const request = event.request
  const url = new URL(request.url)

  // 1. Never touch anything but GET.
  if (request.method !== 'GET') return

  // 2. Never hijack cross-origin requests; they are not ours to cache.
  if (url.origin !== self.location.origin) return

  const path = url.pathname

  // 3. network-only bypass list. Order matters: /api/blog/admin/ MUST be
  //    tested before the public /api/blog/ branch, otherwise admin responses
  //    would fall into the stale-while-revalidate cache.
  if (path.startsWith('/api/blog/admin/')) return
  if (path.startsWith('/api/admin/')) return
  if (path.startsWith('/_auth/')) return
  if (path.startsWith('/s/')) return
  if (path.startsWith('/term/')) return
  if (path === '/admin' || path.startsWith('/admin/')) return

  // 4. Draft-preview URLs carry a short-lived token (`?preview=...`) and must
  //    never be cached. Check both the parsed param and the raw query string so
  //    the literal `preview=` case is always caught.
  if (url.searchParams.has('preview') || url.search.includes('preview=')) return

  // Navigations: network-first, falling back to a cached copy, then the SPA
  // shell, then the offline page.
  if (request.mode === 'navigate') {
    event.respondWith(handleNavigation(request))
    return
  }

  // Public blog reads are the reason the site works offline: keep the last
  // successful response and refresh it in the background.
  if (path.startsWith('/api/blog/')) {
    event.respondWith(staleWhileRevalidate(event))
    return
  }

  // Vite emits content-hashed files under /assets; their content never changes
  // for a given URL, so serve straight from cache.
  if (path.startsWith('/assets/')) {
    event.respondWith(cacheFirst(request))
    return
  }

  // Same-origin static assets: highlight themes, icons, images.
  if (STATIC_PREFIXES.some((prefix) => path.startsWith(prefix)) || IMAGE_EXT.test(path)) {
    event.respondWith(staleWhileRevalidate(event))
    return
  }

  // 5. Anything not whitelisted above: network only, no caching.
})

// ---------------------------------------------------------------------------
// strategies
// ---------------------------------------------------------------------------

async function handleNavigation(request) {
  try {
    const response = await fetch(request)
    if (isCacheable(response)) {
      const cache = await caches.open(CACHE_NAME)
      await cache.put(request, response.clone())
    }
    return response
  } catch {
    const cache = await caches.open(CACHE_NAME)
    const samePath = await cache.match(request)
    if (samePath) return samePath
    const shell = await cache.match('/index.html')
    if (shell) return shell
    const offline = await cache.match('/offline.html')
    if (offline) return offline
    return Response.error()
  }
}

async function staleWhileRevalidate(event) {
  const request = event.request
  const cache = await caches.open(CACHE_NAME)
  const cached = await cache.match(request)

  const network = fetch(request).then(async (response) => {
    if (isCacheable(response)) await cache.put(request, response.clone())
    return response
  })

  if (cached) {
    // Refresh in the background while serving the cached copy.
    event.waitUntil(network.catch(() => {}))
    return cached
  }
  try {
    return await network
  } catch {
    return Response.error()
  }
}

async function cacheFirst(request) {
  const cache = await caches.open(CACHE_NAME)
  const cached = await cache.match(request)
  if (cached) return cached
  try {
    const response = await fetch(request)
    if (isCacheable(response)) await cache.put(request, response.clone())
    return response
  } catch {
    return Response.error()
  }
}

// ---------------------------------------------------------------------------
// response guard
// ---------------------------------------------------------------------------

// Fallback guard required by the spec: never store a response that is not a
// successful same-origin basic response, that carries Set-Cookie, or that is
// marked private / no-store.
function isCacheable(response) {
  if (!response || !response.ok) return false
  if (response.type !== 'basic') return false
  // Note: Set-Cookie is a forbidden response header and is normally not
  // exposed to script, but the check is kept as a defensive backstop.
  if (response.headers.has('Set-Cookie')) return false
  const cacheControl = response.headers.get('Cache-Control') || ''
  if (/(?:^|[,\s])(?:private|no-store)(?:[,\s]|$)/i.test(cacheControl)) return false
  return true
}
