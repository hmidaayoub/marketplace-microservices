/**
 * The guard has four outcomes and three of them are redirects, so each test renders it
 * inside a route table and asserts on where it landed rather than on what it returned.
 *
 * The profile case is the one worth the setup. Registering creates a user but not the
 * CUSTOMER or SELLER profile the business services resolve the caller through, so an
 * account in that state gets 403 from every write. The guard has to send it to the
 * profile form - and must not send it there when it is already on it, which is the
 * infinite redirect this test exists to prevent.
 */

import { screen } from '@testing-library/react'
import { Route, Routes } from 'react-router-dom'
import { describe, expect, it } from 'vitest'

import ProtectedRoute from './protected-route'
import { ANONYMOUS, renderWithProviders, signedIn } from '@/test/render'
import type { AuthState } from '@/store/authSlice'
import type { Role } from '@/api/types'

/** The guard, mounted at `route`, with the destinations it can redirect to also mounted. */
function renderGuard({
  auth,
  route = '/my-requests',
  roles,
}: {
  auth: AuthState
  route?: string
  roles?: Role[]
}) {
  return renderWithProviders(
    <Routes>
      <Route
        path={route}
        element={
          <ProtectedRoute roles={roles}>
            <p>Protected content</p>
          </ProtectedRoute>
        }
      />
      <Route path="/login" element={<p>Login page</p>} />
      <Route path="/profile" element={<p>Profile form</p>} />
      <Route path="/requests" element={<p>Browse requests</p>} />
    </Routes>,
    { auth, route },
  )
}

describe('with no session', () => {
  it('sends a signed-out visitor to the login page', () => {
    renderGuard({ auth: ANONYMOUS })

    expect(screen.getByText('Login page')).toBeInTheDocument()
    expect(screen.queryByText('Protected content')).not.toBeInTheDocument()
  })
})

describe('while the session is still loading', () => {
  it('waits rather than deciding, because the token is restored before /users/me answers', () => {
    renderGuard({ auth: signedIn({ user: null }) })

    expect(screen.getByText('Loading your session…')).toBeInTheDocument()
    // Neither outcome yet: showing the page would leak it, redirecting would bounce a
    // user who is in fact signed in.
    expect(screen.queryByText('Protected content')).not.toBeInTheDocument()
    expect(screen.queryByText('Login page')).not.toBeInTheDocument()
  })
})

describe('with a session but no role profile', () => {
  it('routes to the profile form instead of letting the write 403', () => {
    renderGuard({ auth: signedIn({ hasProfile: false }) })

    expect(screen.getByText('Profile form')).toBeInTheDocument()
  })

  it('renders the profile form itself rather than redirecting to it forever', () => {
    renderGuard({ auth: signedIn({ hasProfile: false }), route: '/profile' })

    expect(screen.getByText('Protected content')).toBeInTheDocument()
  })

  it('does not redirect while hasProfile is still null, which means unchecked', () => {
    renderGuard({ auth: signedIn({ hasProfile: null }) })

    expect(screen.getByText('Protected content')).toBeInTheDocument()
  })
})

describe('with a role requirement', () => {
  it('lets the matching role through', () => {
    renderGuard({ auth: signedIn(), roles: ['CUSTOMER'] })

    expect(screen.getByText('Protected content')).toBeInTheDocument()
  })

  it('sends the wrong role to the public page rather than to login', () => {
    // A seller on a customer-only route is signed in correctly; bouncing them to login
    // would ask them to fix something that is not wrong.
    renderGuard({
      auth: signedIn({ user: { id: 2, email: 's@example.com', role: 'SELLER' } as AuthState['user'] }),
      roles: ['CUSTOMER'],
    })

    expect(screen.getByText('Browse requests')).toBeInTheDocument()
    expect(screen.queryByText('Login page')).not.toBeInTheDocument()
  })

  it('accepts any of several allowed roles', () => {
    renderGuard({
      auth: signedIn({ user: { id: 3, email: 'a@example.com', role: 'ADMIN' } as AuthState['user'] }),
      roles: ['ADMIN', 'CUSTOMER'],
    })

    expect(screen.getByText('Protected content')).toBeInTheDocument()
  })

  it('gates on the session only when no roles are named', () => {
    renderGuard({ auth: signedIn() })

    expect(screen.getByText('Protected content')).toBeInTheDocument()
  })
})

describe('ordering of the checks', () => {
  it('prefers login over the profile form when there is no session at all', () => {
    renderGuard({ auth: { ...ANONYMOUS, hasProfile: false } })

    expect(screen.getByText('Login page')).toBeInTheDocument()
  })

  it('sends a profileless admin-only visitor to the profile form first', () => {
    // The profile check runs before the role check, so the fixable problem is the one
    // the user is asked about.
    renderGuard({ auth: signedIn({ hasProfile: false }), roles: ['ADMIN'] })

    expect(screen.getByText('Profile form')).toBeInTheDocument()
  })
})
