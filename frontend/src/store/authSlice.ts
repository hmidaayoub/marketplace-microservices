/**
 * Session state: the tokens, who they belong to, and the profile the platform requires
 * before any business endpoint will answer.
 *
 * That last part is the non-obvious one. Registering creates a *user*; it does not
 * create the CUSTOMER or SELLER profile that request-service and offer-service resolve
 * the caller through. Until it exists the API answers 403, so the app tracks profile
 * state as part of the session rather than discovering it at the first failed write.
 */

import { createAsyncThunk, createSlice, type PayloadAction } from '@reduxjs/toolkit'

import { api, ApiError } from '@/api/client'
import type { AuthTokens, Customer, Role, Seller, User } from '@/api/types'

const STORAGE_KEY = 'marketplace.session'

interface StoredSession {
  accessToken: string
  refreshToken: string
  /** Epoch ms. Refresh is scheduled off this rather than waiting for a 401. */
  expiresAt: number
}

export interface AuthState {
  accessToken: string | null
  refreshToken: string | null
  expiresAt: number | null
  user: User | null
  /** The role profile, once loaded - it is where the account's name lives. */
  profile: Customer | Seller | null
  /** null = not yet checked; false = checked and absent. */
  hasProfile: boolean | null
  status: 'idle' | 'loading' | 'authenticated'
  error: string | null
}

function readStored(): StoredSession | null {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    return raw ? (JSON.parse(raw) as StoredSession) : null
  } catch {
    // A private window or blocked site data throws rather than returning null.
    return null
  }
}

function writeStored(session: StoredSession | null) {
  try {
    if (session) localStorage.setItem(STORAGE_KEY, JSON.stringify(session))
    else localStorage.removeItem(STORAGE_KEY)
  } catch {
    // Not fatal: the session simply does not survive a reload.
  }
}

const stored = readStored()

const initialState: AuthState = {
  accessToken: stored?.accessToken ?? null,
  refreshToken: stored?.refreshToken ?? null,
  expiresAt: stored?.expiresAt ?? null,
  user: null,
  profile: null,
  hasProfile: null,
  status: 'idle',
  error: null,
}

function toStored(tokens: AuthTokens): StoredSession {
  return {
    accessToken: tokens.accessToken!,
    refreshToken: tokens.refreshToken!,
    // expiresIn is seconds; a minute of slack keeps a refresh from racing expiry.
    expiresAt: Date.now() + (tokens.expiresIn ?? 3600) * 1000 - 60_000,
  }
}

export const login = createAsyncThunk<
  AuthTokens,
  { email: string; password: string },
  { rejectValue: string }
>('auth/login', async (credentials, { rejectWithValue }) => {
  try {
    return await api<AuthTokens>('/api/auth/login', {
      method: 'POST',
      body: credentials,
      skipAuthRetry: true,
    })
  } catch (error) {
    return rejectWithValue(error instanceof ApiError ? error.message : 'Login failed')
  }
})

export const register = createAsyncThunk<
  void,
  { email: string; password: string; phoneNumber: string; role: Role },
  { rejectValue: string }
>('auth/register', async ({ role, ...body }, { dispatch, rejectWithValue }) => {
  const path = role === 'SELLER' ? '/api/auth/register/seller' : '/api/auth/register/customer'
  try {
    await api<User>(path, { method: 'POST', body, skipAuthRetry: true })
    // Registering does not sign you in, so the app does it rather than bouncing the
    // user to a login form they just filled in.
    await dispatch(login({ email: body.email, password: body.password })).unwrap()
  } catch (error) {
    // 409 means the account exists, which is not a dead end - it is the state you reach
    // by registering successfully and then failing the profile step. Say so, because
    // "Email already registered" reads as a rejection when the fix is to sign in.
    if (error instanceof ApiError && error.status === 409) {
      return rejectWithValue(`${error.message}. Sign in instead — you can finish your profile there.`)
    }
    return rejectWithValue(error instanceof ApiError ? error.message : 'Registration failed')
  }
})

/** Loads the account, its role profile, and whether that profile exists yet. */
export const loadSession = createAsyncThunk<
  { user: User; hasProfile: boolean; profile: Customer | Seller | null },
  void,
  { state: { auth: AuthState } }
>('auth/loadSession', async (_, { getState }) => {
  const token = getState().auth.accessToken
  const user = await api<User>('/api/users/me', { token })

  // An admin has no role profile at all - there is nothing to fetch and nothing to gate.
  if (user.role === 'ADMIN') return { user, hasProfile: true, profile: null }

  const path = user.role === 'SELLER' ? '/api/sellers/me' : '/api/customers/me'
  try {
    // The call already had to happen to answer "does a profile exist"; keeping the body
    // is what lets the shell say a name instead of an email address.
    const profile = await api<Customer | Seller>(path, { token })
    return { user, hasProfile: true, profile }
  } catch (error) {
    // 404 or 403 both mean "no profile yet", which is a state to route on, not an error.
    if (error instanceof ApiError && (error.status === 404 || error.status === 403)) {
      return { user, hasProfile: false, profile: null }
    }
    throw error
  }
})

export const logout = createAsyncThunk<void, void, { state: { auth: AuthState } }>(
  'auth/logout',
  async (_, { getState }) => {
    const { accessToken, refreshToken } = getState().auth
    try {
      await api('/api/auth/logout', {
        method: 'POST',
        body: { refreshToken },
        token: accessToken,
        skipAuthRetry: true,
      })
    } catch {
      // The server-side session may already be gone; the client one goes either way.
    }
  },
)

const authSlice = createSlice({
  name: 'auth',
  initialState,
  reducers: {
    tokensRefreshed(state, action: PayloadAction<AuthTokens>) {
      const session = toStored(action.payload)
      state.accessToken = session.accessToken
      state.refreshToken = session.refreshToken
      state.expiresAt = session.expiresAt
      writeStored(session)
    },
    profileCreated(state, action: PayloadAction<Customer | Seller>) {
      state.hasProfile = true
      state.profile = action.payload
    },
    sessionCleared(state) {
      state.accessToken = null
      state.refreshToken = null
      state.expiresAt = null
      state.user = null
      state.profile = null
      state.hasProfile = null
      state.status = 'idle'
      writeStored(null)
    },
  },
  extraReducers: (builder) => {
    builder
      .addCase(login.pending, (state) => {
        state.status = 'loading'
        state.error = null
      })
      .addCase(login.fulfilled, (state, action) => {
        const session = toStored(action.payload)
        state.accessToken = session.accessToken
        state.refreshToken = session.refreshToken
        state.expiresAt = session.expiresAt
        state.status = 'authenticated'
        writeStored(session)
      })
      .addCase(login.rejected, (state, action) => {
        state.status = 'idle'
        state.error = action.payload ?? 'Login failed'
      })
      .addCase(register.pending, (state) => {
        state.status = 'loading'
        state.error = null
      })
      .addCase(register.rejected, (state, action) => {
        state.status = 'idle'
        state.error = action.payload ?? 'Registration failed'
      })
      .addCase(loadSession.fulfilled, (state, action) => {
        state.user = action.payload.user
        state.profile = action.payload.profile
        state.hasProfile = action.payload.hasProfile
        state.status = 'authenticated'
      })
      .addCase(loadSession.rejected, (state) => {
        state.accessToken = null
        state.refreshToken = null
        state.user = null
        state.profile = null
        state.status = 'idle'
        writeStored(null)
      })
      .addCase(logout.fulfilled, (state) => {
        state.accessToken = null
        state.refreshToken = null
        state.expiresAt = null
        state.user = null
        state.profile = null
        state.hasProfile = null
        state.status = 'idle'
        writeStored(null)
      })
  },
})

/**
 * What to call this account on screen.
 *
 * The name is not on the account - it is on the role profile, and the two roles do not
 * name the same kind of thing: a customer is a person, a seller is a store. An admin has
 * no profile at all, so their email address is the only name they have.
 */
export function selectDisplayName(state: { auth: AuthState }): string {
  const { user, profile } = state.auth

  if (profile && 'storeName' in profile && profile.storeName) return profile.storeName

  if (profile && 'firstName' in profile) {
    const fullName = [profile.firstName, profile.lastName].filter(Boolean).join(' ')
    if (fullName) return fullName
  }

  return user?.email ?? ''
}

export const { tokensRefreshed, profileCreated, sessionCleared } = authSlice.actions
export default authSlice.reducer
