/** The shell: role-aware navigation, the unread badge, the theme, and sign-out. */

import { useEffect, useState } from 'react'
import {
  BellIcon,
  ContactIcon,
  GavelIcon,
  KeyIcon,
  LayoutGridIcon,
  LogOutIcon,
  MenuIcon,
  ReceiptTextIcon,
  ScrollTextIcon,
  StoreIcon,
  type LucideIcon,
} from 'lucide-react'
import { NavLink, Outlet, useNavigate } from 'react-router-dom'

import { ModeToggle } from '@/components/mode-toggle'
import { Avatar, AvatarFallback } from '@/components/ui/avatar'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Separator } from '@/components/ui/separator'
import { Sheet, SheetContent, SheetHeader, SheetTitle, SheetTrigger } from '@/components/ui/sheet'
import { useAppDispatch, useAppSelector } from '@/store'
import { logout, selectDisplayName } from '@/store/authSlice'
import { fetchUnreadCount } from '@/store/notificationsSlice'
import { cn } from '@/lib/utils'

/** Notifications arrive over a broker on a two-second relay tick, so the badge polls. */
const UNREAD_POLL_MS = 10_000

interface NavItem {
  to: string
  label: string
  icon: LucideIcon
}

const LINKS: Record<string, NavItem[]> = {
  CUSTOMER: [
    { to: '/requests', label: 'Browse', icon: LayoutGridIcon },
    { to: '/my-requests', label: 'My requests', icon: ScrollTextIcon },
  ],
  SELLER: [
    { to: '/requests', label: 'Browse', icon: LayoutGridIcon },
    { to: '/my-offers', label: 'My offers', icon: ReceiptTextIcon },
    { to: '/contacts', label: 'Contacts', icon: ContactIcon },
  ],
  ADMIN: [
    { to: '/requests', label: 'Browse', icon: LayoutGridIcon },
    { to: '/admin/queue', label: 'Approval queue', icon: GavelIcon },
    { to: '/admin/access', label: 'Contact access', icon: KeyIcon },
  ],
}

const navClass = ({ isActive }: { isActive: boolean }) =>
  cn(
    'inline-flex h-8 items-center gap-1.5 rounded-lg px-2.5 text-sm font-medium transition-colors',
    '[&_svg]:size-4 [&_svg]:shrink-0',
    isActive
      ? 'bg-accent text-accent-foreground'
      : 'text-muted-foreground hover:bg-muted hover:text-foreground',
  )

/** Initials from a person's name or a store's, falling back to an email's first letters. */
function initials(name: string) {
  const words = name.trim().split(/\s+/).filter(Boolean)
  if (words.length > 1) return (words[0][0] + words[1][0]).toUpperCase()
  return (words[0] ?? '?').slice(0, 2).toUpperCase()
}

export default function AppLayout() {
  const dispatch = useAppDispatch()
  const navigate = useNavigate()
  const user = useAppSelector((s) => s.auth.user)
  // A customer is a person and a seller is a store; only an admin, who has no profile,
  // falls back to their email address.
  const displayName = useAppSelector(selectDisplayName)
  const unread = useAppSelector((s) => s.notifications.unread)
  const [menuOpen, setMenuOpen] = useState(false)

  useEffect(() => {
    if (!user) return
    dispatch(fetchUnreadCount())
    const id = setInterval(() => dispatch(fetchUnreadCount()), UNREAD_POLL_MS)
    return () => clearInterval(id)
  }, [dispatch, user])

  const links = user ? (LINKS[user.role ?? ''] ?? []) : []

  const signOut = async () => {
    await dispatch(logout())
    navigate('/login')
  }

  const notifications = (
    <NavLink to="/notifications" className={navClass} onClick={() => setMenuOpen(false)}>
      <BellIcon />
      Notifications
      {unread > 0 && (
        <Badge variant="destructive" className="h-4 min-w-4 px-1 text-[0.6875rem] tabular-nums">
          {unread}
        </Badge>
      )}
    </NavLink>
  )

  return (
    <div className="flex min-h-screen flex-col">
      <header className="sticky top-0 z-40 border-b bg-background/80 backdrop-blur-md">
        <div className="mx-auto flex h-14 max-w-6xl items-center gap-2 px-4">
          {/* The sheet is unmounted while closed, so the nav links exist exactly once. */}
          <Sheet open={menuOpen} onOpenChange={setMenuOpen}>
            <SheetTrigger asChild>
              <Button variant="ghost" size="icon-sm" className="md:hidden" aria-label="Open menu">
                <MenuIcon />
              </Button>
            </SheetTrigger>
            <SheetContent side="left" className="w-72 p-0">
              <SheetHeader className="border-b">
                <SheetTitle className="flex items-center gap-2">
                  <StoreIcon className="size-4 text-primary" />
                  Marketplace
                </SheetTitle>
              </SheetHeader>
              <nav className="flex flex-col gap-1 p-3">
                {links.map((link) => (
                  <NavLink
                    key={link.to}
                    to={link.to}
                    className={navClass}
                    onClick={() => setMenuOpen(false)}
                  >
                    <link.icon />
                    {link.label}
                  </NavLink>
                ))}
                {notifications}
              </nav>
            </SheetContent>
          </Sheet>

          <NavLink to="/requests" className="flex items-center gap-2 font-heading font-semibold">
            <span className="flex size-7 items-center justify-center rounded-lg bg-primary text-primary-foreground">
              <StoreIcon className="size-4" />
            </span>
            <span className="hidden sm:inline">Marketplace</span>
          </NavLink>

          <Separator orientation="vertical" className="mx-1 hidden h-5! md:block" />

          <nav className="hidden flex-1 items-center gap-1 md:flex">
            {links.map((link) => (
              <NavLink key={link.to} to={link.to} className={navClass}>
                <link.icon />
                {link.label}
              </NavLink>
            ))}
            {notifications}
          </nav>

          <div className="ml-auto flex items-center gap-1 md:ml-0">
            <ModeToggle />
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="ghost" size="sm" className="gap-2 px-1.5">
                  <Avatar className="size-6">
                    <AvatarFallback className="text-[0.625rem]">
                      {initials(displayName)}
                    </AvatarFallback>
                  </Avatar>
                  <span className="hidden max-w-44 truncate sm:inline">{displayName}</span>
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-60">
                <DropdownMenuLabel className="flex flex-col gap-0.5">
                  <span className="truncate font-medium">{displayName}</span>
                  {/* The email is what you sign in with, so it stays one level down
                      rather than disappearing. */}
                  <span className="truncate text-xs font-normal text-muted-foreground">
                    {user?.email}
                  </span>
                  <span className="text-xs font-normal text-muted-foreground">
                    Signed in as {user?.role?.toLowerCase()}
                  </span>
                </DropdownMenuLabel>
                <DropdownMenuSeparator />
                <DropdownMenuItem variant="destructive" onSelect={signOut}>
                  <LogOutIcon />
                  Sign out
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </div>
      </header>

      <main className="mx-auto w-full max-w-6xl flex-1 px-4 py-8">
        <Outlet />
      </main>
    </div>
  )
}
