/**
 * The two providers every rendered component in this app sits inside.
 *
 * Components are mounted through here rather than bare, because the interesting ones
 * read the session out of Redux and route on it, and a test that stubs those hooks
 * would be asserting against the stub. `auth` is the only slice preloaded: nothing
 * under test reads the other four, and a fixture for a slice a test does not use is a
 * fixture that goes stale without ever failing.
 */

import { configureStore } from '@reduxjs/toolkit'
import { render, type RenderResult } from '@testing-library/react'
import type { ReactNode } from 'react'
import { Provider } from 'react-redux'
import { MemoryRouter } from 'react-router-dom'

import authReducer, { type AuthState } from '@/store/authSlice'

export const ANONYMOUS: AuthState = {
  accessToken: null,
  refreshToken: null,
  expiresAt: null,
  user: null,
  profile: null,
  hasProfile: null,
  status: 'idle',
  error: null,
}

export function signedIn(overrides: Partial<AuthState> = {}): AuthState {
  return {
    ...ANONYMOUS,
    accessToken: 'access-1',
    refreshToken: 'refresh-1',
    expiresAt: Date.now() + 3_540_000,
    user: { id: 1, email: 'buyer@example.com', role: 'CUSTOMER' } as AuthState['user'],
    hasProfile: true,
    status: 'authenticated',
    ...overrides,
  }
}

export function renderWithProviders(
  ui: ReactNode,
  { auth = ANONYMOUS, route = '/' }: { auth?: AuthState; route?: string } = {},
): RenderResult {
  const store = configureStore({
    reducer: { auth: authReducer },
    preloadedState: { auth },
  })

  return render(
    <Provider store={store}>
      <MemoryRouter initialEntries={[route]}>{ui}</MemoryRouter>
    </Provider>,
  )
}
