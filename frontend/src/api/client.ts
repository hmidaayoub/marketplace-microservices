/**
 * The one place that talks to the backend.
 *
 * Everything goes through the gateway on 8080 - there is no second base URL, because
 * the platform publishes no other port. Identity travels in the Authorization header
 * and nowhere else: the services take the caller from the token's `sub` claim and
 * ignore any id in a header or body, so sending one would be noise at best.
 */

const BASE = import.meta.env.VITE_API_URL ?? 'http://localhost:8080'

/** The error shape every service returns, in all three languages. */
export interface ApiErrorBody {
  message: string
  status: number
}

export class ApiError extends Error {
  readonly status: number

  /**
   * The decoded body, for the errors that carry more than a message - a refused create
   * hands back the open requests it thinks you meant, and throwing that away would
   * leave the caller with nothing to offer.
   */
  readonly body: unknown

  constructor(status: number, message: string, body?: unknown) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.body = body
  }
}

type Method = 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'

interface RequestOptions {
  method?: Method
  body?: unknown
  token?: string | null
  /** Set when the caller handles its own 401 - the login and refresh calls. */
  skipAuthRetry?: boolean
}

/** Swapped in by the auth provider so a 401 can refresh once and retry. */
let onUnauthorized: (() => Promise<string | null>) | null = null

export function setUnauthorizedHandler(handler: (() => Promise<string | null>) | null) {
  onUnauthorized = handler
}

async function parse(response: Response): Promise<unknown> {
  if (response.status === 204) return null
  const text = await response.text()
  if (!text) return null
  try {
    return JSON.parse(text)
  } catch {
    return text
  }
}

export async function api<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const { method = 'GET', body, token, skipAuthRetry } = options

  const send = (bearer: string | null | undefined) => {
    const headers: Record<string, string> = {}
    if (body !== undefined) headers['Content-Type'] = 'application/json'
    if (bearer) headers.Authorization = `Bearer ${bearer}`
    return fetch(`${BASE}${path}`, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
    })
  }

  let response = await send(token)

  // One refresh, one retry. An access token expires on a schedule the app already
  // knows, so this is the fallback for a clock skew or a token revoked mid-session -
  // not the normal path.
  if (response.status === 401 && !skipAuthRetry && onUnauthorized) {
    const refreshed = await onUnauthorized()
    if (refreshed) response = await send(refreshed)
  }

  const payload = await parse(response)

  if (!response.ok) {
    const message =
      payload && typeof payload === 'object' && 'message' in payload
        ? String((payload as ApiErrorBody).message)
        : `Request failed with status ${response.status}`
    throw new ApiError(response.status, message, payload)
  }

  return payload as T
}
