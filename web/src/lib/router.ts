import { useSyncExternalStore } from 'react'

// A tiny history router. The app has five routes, so a dependency is not worth it.
const navEvent = 'scholia:navigate'

function subscribe(onChange: () => void): () => void {
  window.addEventListener('popstate', onChange)
  window.addEventListener(navEvent, onChange)
  return () => {
    window.removeEventListener('popstate', onChange)
    window.removeEventListener(navEvent, onChange)
  }
}

function snapshot(): string {
  return window.location.pathname + window.location.search
}

export function navigate(to: string, opts?: { replace?: boolean }): void {
  if (to === snapshot()) return
  if (opts?.replace) window.history.replaceState(null, '', to)
  else window.history.pushState(null, '', to)
  window.dispatchEvent(new Event(navEvent))
}

export type Location = { path: string; query: URLSearchParams }

export function useLocation(): Location {
  const href = useSyncExternalStore(subscribe, snapshot)
  const url = new URL(href, 'http://local')
  return { path: url.pathname.replace(/\/+$/, '') || '/', query: url.searchParams }
}

export type Route =
  | { name: 'home' }
  | { name: 'chat'; chatId: string | null }
  | { name: 'knowledge'; courseId: string | null }
  | { name: 'settings' }
  | { name: 'notFound' }

export function matchRoute(path: string): Route {
  const parts = path.split('/').filter(Boolean).map(decodeURIComponent)
  if (parts.length === 0) return { name: 'home' }
  const [head, id, ...rest] = parts
  if (rest.length > 0) return { name: 'notFound' }
  if (head === 'chat') return { name: 'chat', chatId: id ?? null }
  if (head === 'knowledge') return { name: 'knowledge', courseId: id ?? null }
  if (head === 'settings' && id === undefined) return { name: 'settings' }
  return { name: 'notFound' }
}
