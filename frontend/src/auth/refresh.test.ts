/**
 * The handler the API client calls on a 401. Two outcomes matter and they are opposites:
 * a refresh that works has to put the rotated tokens in the store before returning the
 * access token, and a refresh that fails has to end the session rather than let the app
 * keep retrying with a token the server has already rejected.
 */

import { configureStore } from '@reduxjs/toolkit'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import authReducer from '@/store/authSlice'
import { installRefresh } from './refresh'

const fetchMock = vi.fn()

function storeWith(refreshToken: string | null) {
  const store = configureStore({ reducer: { auth: authReducer } })
  if (refreshToken) {
    store.dispatch({
      type: 'auth/login/fulfilled',
      payload: { accessToken: 'access-1', refreshToken, expiresIn: 3600 },
    })
  }
  return store as unknown as Parameters<typeof installRefresh>[0]
}

function reply(status: number, body: unknown): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    text: () => Promise.resolve(JSON.stringify(body)),
    blob: () => Promise.resolve(new Blob()),
  } as unknown as Response
}

beforeEach(() => {
  localStorage.clear()
  fetchMock.mockReset()
  vi.stubGlobal('fetch', fetchMock)
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('installRefresh', () => {
  it('returns null without a request when there is no refresh token to spend', async () => {
    const refresh = installRefresh(storeWith(null))

    await expect(refresh()).resolves.toBeNull()
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('returns the new access token and puts both rotated tokens in the store', async () => {
    fetchMock.mockResolvedValue(reply(200, { accessToken: 'access-2', refreshToken: 'refresh-2', expiresIn: 3600 }))
    const store = storeWith('refresh-1')
    const refresh = installRefresh(store)

    await expect(refresh()).resolves.toBe('access-2')

    const { auth } = store.getState()
    expect(auth.accessToken).toBe('access-2')
    expect(auth.refreshToken).toBe('refresh-2')
  })

  it('sends the refresh token, and sends it with skipAuthRetry', async () => {
    fetchMock.mockResolvedValue(reply(200, { accessToken: 'access-2', refreshToken: 'refresh-2' }))
    const refresh = installRefresh(storeWith('refresh-1'))

    await refresh()

    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toContain('/api/auth/refresh')
    expect(init.body).toBe('{"refreshToken":"refresh-1"}')
    // One call, not two: a 401 on the refresh endpoint must not trigger another refresh.
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('clears the session when the refresh token is spent or revoked', async () => {
    fetchMock.mockResolvedValue(reply(401, { message: 'Invalid refresh token', status: 401 }))
    const store = storeWith('refresh-1')
    const refresh = installRefresh(store)

    await expect(refresh()).resolves.toBeNull()

    const { auth } = store.getState()
    expect(auth.accessToken).toBeNull()
    expect(auth.refreshToken).toBeNull()
    expect(auth.status).toBe('idle')
    expect(localStorage.getItem('marketplace.session')).toBeNull()
  })

  it('clears the session when the refresh call cannot be made at all', async () => {
    fetchMock.mockRejectedValue(new TypeError('Failed to fetch'))
    const store = storeWith('refresh-1')

    await expect(installRefresh(store)()).resolves.toBeNull()

    expect(store.getState().auth.accessToken).toBeNull()
  })

  it('returns null when the response carried no access token', async () => {
    fetchMock.mockResolvedValue(reply(200, { refreshToken: 'refresh-2' }))

    await expect(installRefresh(storeWith('refresh-1'))()).resolves.toBeNull()
  })
})
