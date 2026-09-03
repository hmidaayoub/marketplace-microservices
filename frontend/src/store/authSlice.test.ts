/**
 * The session reducer, and the one thing about it that is not obvious from the type:
 * every path that changes the tokens also has to write them to localStorage, and every
 * path that ends the session has to remove them. A reducer that updates state but not
 * storage passes any assertion about state and still signs the user out on reload.
 */

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { AuthTokens, Customer, Seller, User } from '@/api/types'
import reducer, {
  loadSession,
  login,
  logout,
  profileCreated,
  selectDisplayName,
  sessionCleared,
  tokensRefreshed,
  type AuthState,
} from './authSlice'

const STORAGE_KEY = 'marketplace.session'

const TOKENS: AuthTokens = {
  accessToken: 'access-1',
  refreshToken: 'refresh-1',
  expiresIn: 3600,
}

/** The state the reducer starts from, without depending on what storage held at import. */
function anonymous(): AuthState {
  return {
    accessToken: null,
    refreshToken: null,
    expiresAt: null,
    user: null,
    profile: null,
    hasProfile: null,
    status: 'idle',
    error: null,
  }
}

function authenticated(): AuthState {
  return {
    ...anonymous(),
    accessToken: 'access-1',
    refreshToken: 'refresh-1',
    expiresAt: Date.now() + 3_540_000,
    user: { id: 1, email: 'buyer@example.com', role: 'CUSTOMER' } as User,
    hasProfile: true,
    status: 'authenticated',
  }
}

function storedSession() {
  const raw = localStorage.getItem(STORAGE_KEY)
  return raw ? JSON.parse(raw) : null
}

beforeEach(() => {
  localStorage.clear()
})

describe('login', () => {
  it('reports loading and clears the previous error', () => {
    const state = reducer({ ...anonymous(), error: 'Bad credentials' }, login.pending('', { email: '', password: '' }))

    expect(state.status).toBe('loading')
    expect(state.error).toBeNull()
  })

  it('stores the tokens in state and in localStorage', () => {
    const state = reducer(anonymous(), login.fulfilled(TOKENS, '', { email: '', password: '' }))

    expect(state.status).toBe('authenticated')
    expect(state.accessToken).toBe('access-1')
    expect(state.refreshToken).toBe('refresh-1')
    expect(storedSession()).toMatchObject({ accessToken: 'access-1', refreshToken: 'refresh-1' })
  })

  it('surfaces the rejection message and returns to idle', () => {
    const action = login.rejected(null, '', { email: '', password: '' }, 'Bad credentials')
    const state = reducer({ ...anonymous(), status: 'loading' }, action)

    expect(state.status).toBe('idle')
    expect(state.error).toBe('Bad credentials')
  })

  it('has something to say even when the rejection carried no payload', () => {
    const action = login.rejected(new Error('boom'), '', { email: '', password: '' })
    const state = reducer(anonymous(), action)

    expect(state.error).toBe('Login failed')
  })
})

describe('when the session expires', () => {
  // On a frozen clock, deliberately. Read off the wall clock, `expiresAt - Date.now()`
  // is only exactly 3_540_000 while no millisecond elapses between the two readings -
  // an assertion that passes about a thousand times for every time it does not, which
  // is the worst failure rate a test can have.
  const NOW = new Date('2026-09-03T12:00:00.000Z')

  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(NOW)
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('expires a minute early, so a refresh does not race the token it is refreshing', () => {
    const state = reducer(anonymous(), login.fulfilled(TOKENS, '', { email: '', password: '' }))

    // 3600s of validity, less the 60s of slack toStored subtracts.
    expect(state.expiresAt! - NOW.getTime()).toBe(3_540_000)
  })

  it('falls back to an hour when the server sends no expiresIn', () => {
    const state = reducer(
      anonymous(),
      login.fulfilled({ accessToken: 'a', refreshToken: 'r' }, '', { email: '', password: '' }),
    )

    expect(state.expiresAt! - NOW.getTime()).toBe(3_540_000)
  })

  it('applies the same slack to a refreshed token', () => {
    const state = reducer(
      authenticated(),
      tokensRefreshed({ accessToken: 'access-2', refreshToken: 'refresh-2', expiresIn: 900 }),
    )

    expect(state.expiresAt! - NOW.getTime()).toBe(840_000)
  })

  it('can hand back an already-past expiry for a token the server says is nearly dead', () => {
    // 30s of validity minus 60s of slack is negative, and that is the honest answer:
    // the app should refresh immediately rather than schedule into the past and wait.
    const state = reducer(
      anonymous(),
      login.fulfilled({ accessToken: 'a', refreshToken: 'r', expiresIn: 30 }, '', { email: '', password: '' }),
    )

    expect(state.expiresAt! - NOW.getTime()).toBe(-30_000)
  })
})

describe('tokensRefreshed', () => {
  it('replaces both tokens in storage, not only the access token', () => {
    localStorage.setItem(
      STORAGE_KEY,
      JSON.stringify({ accessToken: 'access-1', refreshToken: 'refresh-1', expiresAt: 1 }),
    )

    const state = reducer(
      authenticated(),
      tokensRefreshed({ accessToken: 'access-2', refreshToken: 'refresh-2', expiresIn: 3600 }),
    )

    expect(state.accessToken).toBe('access-2')
    expect(state.refreshToken).toBe('refresh-2')
    // The rotated refresh token is the one that matters: keeping the old one in storage
    // means the next reload starts from a token the server has already spent.
    expect(storedSession()).toMatchObject({ accessToken: 'access-2', refreshToken: 'refresh-2' })
  })

  it('leaves the loaded user in place - a refresh is not a new session', () => {
    const state = reducer(authenticated(), tokensRefreshed(TOKENS))

    expect(state.user?.email).toBe('buyer@example.com')
    expect(state.status).toBe('authenticated')
  })
})

describe('ending a session', () => {
  it('sessionCleared empties both state and storage', () => {
    localStorage.setItem(STORAGE_KEY, JSON.stringify({ accessToken: 'access-1' }))

    const state = reducer(authenticated(), sessionCleared())

    expect(state.accessToken).toBeNull()
    expect(state.refreshToken).toBeNull()
    expect(state.user).toBeNull()
    expect(state.hasProfile).toBeNull()
    expect(state.status).toBe('idle')
    expect(storedSession()).toBeNull()
  })

  it('logout does the same once the server call has settled', () => {
    localStorage.setItem(STORAGE_KEY, JSON.stringify({ accessToken: 'access-1' }))

    const state = reducer(authenticated(), logout.fulfilled(undefined, ''))

    expect(state.accessToken).toBeNull()
    expect(state.status).toBe('idle')
    expect(storedSession()).toBeNull()
  })

  it('a failed loadSession clears storage too, so the bad token is not retried on reload', () => {
    localStorage.setItem(STORAGE_KEY, JSON.stringify({ accessToken: 'access-1' }))

    const state = reducer(authenticated(), loadSession.rejected(new Error('401'), ''))

    expect(state.accessToken).toBeNull()
    expect(state.user).toBeNull()
    expect(storedSession()).toBeNull()
  })
})

describe('loadSession', () => {
  it('records that a customer has no profile yet, which is a route and not an error', () => {
    const user = { id: 1, email: 'new@example.com', role: 'CUSTOMER' } as User
    const state = reducer(
      { ...anonymous(), accessToken: 'access-1' },
      loadSession.fulfilled({ user, hasProfile: false, profile: null }, ''),
    )

    expect(state.hasProfile).toBe(false)
    expect(state.status).toBe('authenticated')
    expect(state.profile).toBeNull()
  })

  it('keeps the profile body, which is where the display name lives', () => {
    const user = { id: 1, email: 'buyer@example.com', role: 'CUSTOMER' } as User
    const profile = { id: 9, firstName: 'Ada', lastName: 'Lovelace' } as Customer

    const state = reducer(anonymous(), loadSession.fulfilled({ user, hasProfile: true, profile }, ''))

    expect(state.profile).toEqual(profile)
  })
})

describe('profileCreated', () => {
  it('flips hasProfile without another round trip', () => {
    const state = reducer(
      { ...authenticated(), hasProfile: false },
      profileCreated({ id: 9, firstName: 'Ada' } as Customer),
    )

    expect(state.hasProfile).toBe(true)
    expect(state.profile).toMatchObject({ firstName: 'Ada' })
  })
})

describe('selectDisplayName', () => {
  const withProfile = (profile: Customer | Seller | null, user: User | null = { id: 1, email: 'me@example.com', role: 'CUSTOMER' } as User) =>
    selectDisplayName({ auth: { ...anonymous(), user, profile } })

  it('prefers a store name, because a seller is a shop and not a person', () => {
    expect(withProfile({ id: 1, storeName: 'Ada Supplies' } as Seller)).toBe('Ada Supplies')
  })

  it('joins a customer first and last name', () => {
    expect(withProfile({ id: 1, firstName: 'Ada', lastName: 'Lovelace' } as Customer)).toBe('Ada Lovelace')
  })

  it('uses the half of the name that exists', () => {
    expect(withProfile({ id: 1, firstName: 'Ada' } as Customer)).toBe('Ada')
  })

  it('falls back to the email when a customer profile carries no name at all', () => {
    expect(withProfile({ id: 1, firstName: '', lastName: '' } as Customer)).toBe('me@example.com')
  })

  it('falls back to the email for an admin, who has no profile to name them', () => {
    expect(withProfile(null, { id: 1, email: 'admin@example.com', role: 'ADMIN' } as User)).toBe('admin@example.com')
  })

  it('is empty rather than undefined before the user has loaded', () => {
    expect(withProfile(null, null)).toBe('')
  })

  it('ignores a seller profile whose storeName is blank', () => {
    expect(withProfile({ id: 1, storeName: '' } as Seller)).toBe('me@example.com')
  })
})
