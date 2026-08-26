/** The shell: role-aware navigation, the unread badge, and sign-out. */

import { useEffect } from 'react'
import { NavLink, Outlet, useNavigate } from 'react-router-dom'

import { useAppDispatch, useAppSelector } from '../store'
import { logout } from '../store/authSlice'
import { fetchUnreadCount } from '../store/notificationsSlice'
import { Button } from './ui'

/** Notifications arrive over a broker on a two-second relay tick, so the badge polls. */
const UNREAD_POLL_MS = 10_000

const LINKS: Record<string, { to: string; label: string }[]> = {
  CUSTOMER: [
    { to: '/requests', label: 'Browse' },
    { to: '/my-requests', label: 'My requests' },
  ],
  SELLER: [
    { to: '/requests', label: 'Browse' },
    { to: '/my-offers', label: 'My offers' },
    { to: '/contacts', label: 'Contacts' },
  ],
  ADMIN: [
    { to: '/requests', label: 'Browse' },
    { to: '/admin/queue', label: 'Approval queue' },
    { to: '/admin/access', label: 'Contact access' },
  ],
}

export default function Layout() {
  const dispatch = useAppDispatch()
  const navigate = useNavigate()
  const user = useAppSelector((s) => s.auth.user)
  const unread = useAppSelector((s) => s.notifications.unread)

  useEffect(() => {
    if (!user) return
    dispatch(fetchUnreadCount())
    const id = setInterval(() => dispatch(fetchUnreadCount()), UNREAD_POLL_MS)
    return () => clearInterval(id)
  }, [dispatch, user])

  const links = user ? (LINKS[user.role ?? ''] ?? []) : []

  const linkClass = ({ isActive }: { isActive: boolean }) =>
    `rounded-md px-3 py-2 text-sm font-medium ${
      isActive ? 'bg-brand-50 text-brand-700' : 'text-slate-600 hover:bg-slate-100'
    }`

  return (
    <div className="min-h-screen">
      <header className="border-b border-slate-200 bg-white">
        <div className="mx-auto flex max-w-6xl items-center gap-2 px-4 py-3">
          <span className="mr-4 text-base font-semibold text-slate-900">Marketplace</span>

          <nav className="flex flex-1 items-center gap-1">
            {links.map((link) => (
              <NavLink key={link.to} to={link.to} className={linkClass}>
                {link.label}
              </NavLink>
            ))}
            <NavLink to="/notifications" className={linkClass}>
              Notifications
              {unread > 0 && (
                <span className="ml-1.5 rounded-full bg-red-600 px-1.5 py-0.5 text-xs text-white">
                  {unread}
                </span>
              )}
            </NavLink>
          </nav>

          <span className="text-sm text-slate-500">
            {user?.email} · <span className="font-medium text-slate-700">{user?.role}</span>
          </span>
          <Button
            variant="ghost"
            onClick={async () => {
              await dispatch(logout())
              navigate('/login')
            }}
          >
            Sign out
          </Button>
        </div>
      </header>

      <main className="mx-auto max-w-6xl px-4 py-6">
        <Outlet />
      </main>
    </div>
  )
}
