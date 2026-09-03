/**
 * The client's job is not "call fetch". It is the four decisions layered on top:
 * which headers a body implies, what a non-JSON error body degrades to, when a 401 is
 * worth one retry, and what an error carries besides its message.
 *
 * fetch is stubbed rather than served by a local server, because every one of those
 * decisions is about the request that goes out and the response object that comes back,
 * and neither needs a socket to be true.
 */

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { ApiError, api, multipart, setUnauthorizedHandler } from './client'

const BASE = 'http://localhost:8080'

/**
 * A stand-in for Response carrying only what the client reads. A real Response would
 * work too, but this makes the fixture say exactly which four members the code depends
 * on - and keeps `body` assertable after the call.
 */
function reply(
  status: number,
  body?: unknown,
  { text }: { text?: string } = {},
): Response {
  const payload = text ?? (body === undefined ? '' : JSON.stringify(body))
  return {
    ok: status >= 200 && status < 300,
    status,
    text: () => Promise.resolve(payload),
    blob: () => Promise.resolve(new Blob([payload])),
  } as unknown as Response
}

let fetchMock: ReturnType<typeof vi.fn>

beforeEach(() => {
  fetchMock = vi.fn()
  vi.stubGlobal('fetch', fetchMock)
})

afterEach(() => {
  // Left set, the handler from a retry test refreshes tokens in every test after it.
  setUnauthorizedHandler(null)
  vi.unstubAllGlobals()
})

/** The RequestInit the client passed on its nth call. */
function sentInit(call = 0): RequestInit {
  return fetchMock.mock.calls[call][1] as RequestInit
}

function sentHeaders(call = 0): Record<string, string> {
  return sentInit(call).headers as Record<string, string>
}

describe('the request it builds', () => {
  it('prefixes the gateway origin, because there is no second base URL', async () => {
    fetchMock.mockResolvedValue(reply(200, { id: 1 }))

    await api('/api/users/me')

    expect(fetchMock.mock.calls[0][0]).toBe(`${BASE}/api/users/me`)
  })

  it('defaults to GET and sends no body and no Content-Type', async () => {
    fetchMock.mockResolvedValue(reply(200, {}))

    await api('/api/users/me')

    expect(sentInit().method).toBe('GET')
    expect(sentInit().body).toBeUndefined()
    expect(sentHeaders()['Content-Type']).toBeUndefined()
  })

  it('sends a plain body as JSON', async () => {
    fetchMock.mockResolvedValue(reply(200, {}))

    await api('/api/auth/login', { method: 'POST', body: { email: 'a@b.c' } })

    expect(sentHeaders()['Content-Type']).toBe('application/json')
    expect(sentInit().body).toBe('{"email":"a@b.c"}')
  })

  it('leaves FormData alone, because only the browser knows the multipart boundary', async () => {
    fetchMock.mockResolvedValue(reply(200, {}))
    const form = multipart({ title: 'A drill' }, null)

    await api('/api/requests', { method: 'POST', body: form })

    // Setting Content-Type here by hand would produce a boundary-less header the server
    // cannot parse the body against.
    expect(sentHeaders()['Content-Type']).toBeUndefined()
    expect(sentInit().body).toBe(form)
  })

  it('carries identity in the Authorization header and nowhere else', async () => {
    fetchMock.mockResolvedValue(reply(200, {}))

    await api('/api/customers/me', { token: 'access-1' })

    expect(sentHeaders().Authorization).toBe('Bearer access-1')
  })

  it('omits Authorization entirely when there is no token', async () => {
    fetchMock.mockResolvedValue(reply(200, {}))

    await api('/api/requests')

    expect(sentHeaders().Authorization).toBeUndefined()
  })
})

describe('the response it returns', () => {
  it('decodes a JSON body', async () => {
    fetchMock.mockResolvedValue(reply(200, { id: 7, title: 'A drill' }))

    await expect(api('/api/requests/7')).resolves.toEqual({ id: 7, title: 'A drill' })
  })

  it('returns null for 204, which the delete endpoints answer with', async () => {
    fetchMock.mockResolvedValue(reply(204))

    await expect(api('/api/offers/1')).resolves.toBeNull()
  })

  it('returns null for a 200 with an empty body', async () => {
    fetchMock.mockResolvedValue(reply(200, undefined, { text: '' }))

    await expect(api('/api/health')).resolves.toBeNull()
  })

  it('hands back text it cannot parse rather than throwing on it', async () => {
    fetchMock.mockResolvedValue(reply(200, undefined, { text: 'pong' }))

    await expect(api('/api/ping')).resolves.toBe('pong')
  })

  it('returns bytes when asked for a blob', async () => {
    fetchMock.mockResolvedValue(reply(200, undefined, { text: 'PNGDATA' }))

    const result = await api<Blob>('/api/images/1', { responseType: 'blob' })

    expect(result).toBeInstanceOf(Blob)
  })
})

describe('the errors it throws', () => {
  it('uses the platform error shape all three languages return', async () => {
    fetchMock.mockResolvedValue(reply(404, { message: 'Request not found', status: 404 }))

    await expect(api('/api/requests/99')).rejects.toBeInstanceOf(ApiError)
    await expect(api('/api/requests/99')).rejects.toThrow('Request not found')
  })

  it('sets status on the error, which the callers branch on', async () => {
    fetchMock.mockResolvedValue(reply(403, { message: 'Forbidden', status: 403 }))

    await expect(api('/api/customers/me')).rejects.toMatchObject({ status: 403, name: 'ApiError' })
  })

  it('keeps the whole body, because a refused create hands back the requests it meant', async () => {
    const body = { message: 'Similar requests are open', status: 409, candidates: [{ id: 3 }, { id: 4 }] }
    fetchMock.mockResolvedValue(reply(409, body))

    await expect(api('/api/requests', { method: 'POST', body: {} })).rejects.toMatchObject({ body })
  })

  it('describes the status when the body carries no message', async () => {
    fetchMock.mockResolvedValue(reply(502, undefined, { text: '<html>bad gateway</html>' }))

    await expect(api('/api/requests')).rejects.toThrow('Request failed with status 502')
  })

  it('still produces the platform error shape for a failed blob request', async () => {
    fetchMock.mockResolvedValue(reply(404, { message: 'No image', status: 404 }))

    // Read before the ok check, so the error body is not left unread behind a blob().
    await expect(api('/api/images/1', { responseType: 'blob' })).rejects.toThrow('No image')
  })
})

describe('the one refresh and the one retry', () => {
  it('refreshes on a 401 and replays the request with the new token', async () => {
    fetchMock.mockResolvedValueOnce(reply(401, { message: 'Expired', status: 401 }))
    fetchMock.mockResolvedValueOnce(reply(200, { id: 1 }))
    setUnauthorizedHandler(vi.fn().mockResolvedValue('access-2'))

    await expect(api('/api/users/me', { token: 'access-1' })).resolves.toEqual({ id: 1 })

    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(sentHeaders(0).Authorization).toBe('Bearer access-1')
    expect(sentHeaders(1).Authorization).toBe('Bearer access-2')
  })

  it('gives up after one retry rather than looping on a token the server keeps refusing', async () => {
    fetchMock.mockResolvedValue(reply(401, { message: 'Expired', status: 401 }))
    setUnauthorizedHandler(vi.fn().mockResolvedValue('access-2'))

    await expect(api('/api/users/me', { token: 'access-1' })).rejects.toMatchObject({ status: 401 })

    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('throws the original 401 when there is nothing left to refresh with', async () => {
    fetchMock.mockResolvedValue(reply(401, { message: 'Expired', status: 401 }))
    setUnauthorizedHandler(vi.fn().mockResolvedValue(null))

    await expect(api('/api/users/me', { token: 'access-1' })).rejects.toMatchObject({ status: 401 })

    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('does not refresh for the calls that handle their own 401', async () => {
    fetchMock.mockResolvedValue(reply(401, { message: 'Bad credentials', status: 401 }))
    const handler = vi.fn().mockResolvedValue('access-2')
    setUnauthorizedHandler(handler)

    // Login and refresh set skipAuthRetry: without it, a wrong password would spend the
    // refresh token trying to fix itself.
    await expect(
      api('/api/auth/login', { method: 'POST', body: {}, skipAuthRetry: true }),
    ).rejects.toMatchObject({ status: 401 })

    expect(handler).not.toHaveBeenCalled()
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('does not retry a 403, which means the profile is missing and not the token', async () => {
    fetchMock.mockResolvedValue(reply(403, { message: 'Forbidden', status: 403 }))
    const handler = vi.fn().mockResolvedValue('access-2')
    setUnauthorizedHandler(handler)

    await expect(api('/api/customers/me', { token: 'access-1' })).rejects.toMatchObject({ status: 403 })

    expect(handler).not.toHaveBeenCalled()
  })
})

describe('multipart', () => {
  it('puts the JSON whole into one part rather than spreading it across form keys', () => {
    const form = multipart({ title: 'A drill', quantity: 2 }, null)

    expect(form.get('payload')).toBe('{"title":"A drill","quantity":2}')
  })

  it('returns FormData even with no image, so the form posts one way', () => {
    const form = multipart({ title: 'A drill' }, null)

    expect(form).toBeInstanceOf(FormData)
    expect(form.has('image')).toBe(false)
  })

  it('names the file part so both the Go and the Python service find it', () => {
    const image = new Blob(['PNG'], { type: 'image/png' })
    const form = multipart({ title: 'A drill' }, image)

    expect(form.get('image')).toBeInstanceOf(Blob)
  })
})
