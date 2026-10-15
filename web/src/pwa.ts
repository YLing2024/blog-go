/// <reference types="vite/client" />

// Service worker registration.
//
// Registration happens in production builds only. In development, any worker
// left over from a previous production preview is unregistered so the dev
// server is never served from a stale cache.
//
// Update strategy: automatic takeover plus an automatic one-time reload. The
// worker calls skipWaiting() on install and clients.claim() on activate, so a
// rebuilt deployment takes control of an already-open page; this file reloads
// the page once when control changes, which keeps the on-screen content and the
// served worker in step. The reload is intentionally not gated behind a prompt
// because the update has to be seen even if the user never interacts.

export function registerServiceWorker(): void {
  if (!('serviceWorker' in navigator)) return

  if (!import.meta.env.PROD) {
    void navigator.serviceWorker.getRegistrations().then((registrations) => {
      for (const registration of registrations) {
        void registration.unregister()
      }
    })
    return
  }

  // Reload once when an updated worker takes over. Guarded by an existing
  // controller so the very first install (which also fires controllerchange
  // when the worker claims the page) does not cause a spurious reload.
  let reloading = false
  if (navigator.serviceWorker.controller) {
    navigator.serviceWorker.addEventListener('controllerchange', () => {
      if (reloading) return
      reloading = true
      window.location.reload()
    })
  }

  window.addEventListener('load', () => {
    void navigator.serviceWorker.register('/sw.js', { scope: '/' })
  })
}
