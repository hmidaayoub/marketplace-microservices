/**
 * Gates a route on a session, a role, and - the easy one to miss - a role profile.
 *
 * Registering creates a user but not the CUSTOMER or SELLER profile the business
 * services resolve the caller through, so a freshly registered account gets 403 from
 * every write until it exists. Rather than surface that as an error, the guard routes
 * to the profile form, which is what the user actually has to do next.
 */

import type { ReactNode } from 'react'
import { Navigate, useLocation } from 'react-router-dom'

import type { Role } from '../api/types'
import { useAppSelector } from '../store'

export default function ProtectedRoute({
  children,
  roles,
}: {
  children: ReactNode
  roles?: Role[]
}) {
  const location = useLocation()
  const { accessToken, user, hasProfile } = useAppSelector((s) => s.auth)

  if (!accessToken) return <Navigate to="/login" state={{ from: location }} replace />

  // The session is restored from storage before /api/users/me has answered.
  if (!user) {
    return <p className="py-12 text-center text-sm text-slate-500">Loading your session…</p>
  }

  if (hasProfile === false && location.pathname !== '/profile') {
    return <Navigate to="/profile" replace />
  }

  if (roles && !roles.includes(user.role as Role)) {
    return <Navigate to="/requests" replace />
  }

  return <>{children}</>
}
