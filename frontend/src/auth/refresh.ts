/**
 * Connects the API client's 401 hook to the store.
 *
 * Kept out of the client so that module stays free of store imports, and out of the
 * slice so the slice stays free of the client's global handler.
 */

import { api } from '../api/client'
import type { AuthTokens } from '../api/types'
import { sessionCleared, tokensRefreshed } from '../store/authSlice'
import type { store as Store } from '../store'

export function installRefresh(store: typeof Store) {
  return async (): Promise<string | null> => {
    const { refreshToken } = store.getState().auth
    if (!refreshToken) return null

    try {
      const tokens = await api<AuthTokens>('/api/auth/refresh', {
        method: 'POST',
        body: { refreshToken },
        skipAuthRetry: true,
      })
      store.dispatch(tokensRefreshed(tokens))
      return tokens.accessToken ?? null
    } catch {
      // The refresh token is spent or revoked; there is nothing left to retry with.
      store.dispatch(sessionCleared())
      return null
    }
  }
}
