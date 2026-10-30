import { createContext, useContext, type PropsWithChildren, type ReactNode } from 'react'
import { setAdminReadOnly } from '../api'

export type AdminRole = 'admin' | 'viewer'

export type AdminSession = {
  role: AdminRole
  /** Viewer key name, or "admin". */
  name: string
}

type AdminSessionValue = AdminSession & { readOnly: boolean }

const ADMIN_SESSION: AdminSessionValue = { role: 'admin', name: 'admin', readOnly: false }

const AdminSessionContext = createContext<AdminSessionValue>(ADMIN_SESSION)

/**
 * Resolves who the stored key belongs to via GET /api/admin/me. Returns null when
 * the answer is unknown (network error, older backend without /me); callers keep
 * their previous session in that case. The backend enforces read-only access on
 * its own, so this only drives what the UI shows.
 */
export async function fetchAdminSession(key: string): Promise<AdminSession | null> {
  if (!key) return null
  try {
    const res = await fetch('/api/admin/me', { headers: { 'X-Admin-Key': key }, cache: 'no-store' })
    if (!res.ok) return null
    const body = (await res.json()) as Partial<AdminSession>
    return body.role === 'viewer'
      ? { role: 'viewer', name: typeof body.name === 'string' ? body.name : '' }
      : { role: 'admin', name: 'admin' }
  } catch {
    return null
  }
}

export function AdminSessionProvider({ session, children }: PropsWithChildren<{ session: AdminSession | null }>) {
  const value: AdminSessionValue = session?.role === 'viewer'
    ? { ...session, readOnly: true }
    : ADMIN_SESSION
  // Synchronous so child effects that fire API calls on mount already see it.
  setAdminReadOnly(value.readOnly)
  return <AdminSessionContext.Provider value={value}>{children}</AdminSessionContext.Provider>
}

export function useAdminSession(): AdminSessionValue {
  return useContext(AdminSessionContext)
}

/** True when the signed-in key is a read-only viewer key. */
export function useReadOnly(): boolean {
  return useContext(AdminSessionContext).readOnly
}

/** Renders its children only for full admins; read-only viewers see `fallback`. */
export function WriteOnly({ children, fallback = null }: { children: ReactNode; fallback?: ReactNode }) {
  return <>{useReadOnly() ? fallback : children}</>
}
