// Pages a read-only viewer key may open: the dashboard, the account pool
// (every channel view) and usage stats. Must stay within the backend allowlist
// in admin/viewer_access.go; anything else is redirected to the dashboard.

/** Top-level nav entries shown to viewers. */
export const VIEWER_NAV_PATHS: ReadonlySet<string> = new Set(['/', '/accounts', '/usage'])

const VIEWER_ROUTE_PREFIXES = ['/accounts', '/usage'] as const
// The Codex invite view is entirely write actions.
const VIEWER_BLOCKED_ROUTES: ReadonlySet<string> = new Set(['/accounts/invite'])

export function isViewerRoute(pathname: string): boolean {
  const path = pathname.replace(/\/+$/, '') || '/'
  if (path === '/') return true
  if (VIEWER_BLOCKED_ROUTES.has(path)) return false
  return VIEWER_ROUTE_PREFIXES.some((prefix) => path === prefix || path.startsWith(`${prefix}/`))
}
