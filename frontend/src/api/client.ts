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
  /** A plain value is sent as JSON; FormData is sent as multipart, for the endpoints
   *  that take a picture alongside their fields. */
  body?: unknown
  token?: string | null
  /** Set when the caller handles its own 401 - the login and refresh calls. */
  skipAuthRetry?: boolean
  /** 'blob' for the image endpoints, which answer with bytes rather than JSON. The
   *  refresh-and-retry above still applies: an image behind a token can 401 like
   *  anything else. */
  responseType?: 'json' | 'blob'
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
  const { method = 'GET', body, token, skipAuthRetry, responseType = 'json' } = options

  const send = (bearer: string | null | undefined) => {
    const headers: Record<string, string> = {}
    // FormData sets its own Content-Type, and it has to: the header carries the
    // multipart boundary, which only the browser knows. Setting it here by hand would
    // produce a boundary-less header the server cannot parse the body against.
    if (body !== undefined && !(body instanceof FormData)) {
      headers['Content-Type'] = 'application/json'
    }
    if (bearer) headers.Authorization = `Bearer ${bearer}`
    return fetch(`${BASE}${path}`, {
      method,
      headers,
      body: body === undefined ? undefined : body instanceof FormData ? body : JSON.stringify(body),
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

  // Read before the ok check either way, so a failed image request still produces the
  // platform's error shape rather than an unread body.
  const payload = response.ok && responseType === 'blob' ? await response.blob() : await parse(response)

  if (!response.ok) {
    const message =
      payload && typeof payload === 'object' && 'message' in payload
        ? String((payload as ApiErrorBody).message)
        : `Request failed with status ${response.status}`
    throw new ApiError(response.status, message, payload)
  }

  return payload as T
}

/**
 * The body for an endpoint that accepts a picture beside its JSON.
 *
 * The two services that take one - request-service in Go, offer-service in Python -
 * agree on the shape: the fields in a part named `payload`, the file in a part named
 * `image`. The JSON stays whole in one part rather than being spread across form keys,
 * because these bodies are not flat and a server would otherwise need a second parser
 * for the multipart case that could disagree with the first about what is valid.
 *
 * Called with no image it still returns FormData, and that is deliberate: the form
 * posts the same way whether or not a picture was chosen, so there is one request path
 * to get right rather than two.
 */
export function multipart(payload: unknown, image: Blob | null): FormData {
  const form = new FormData()
  form.append('payload', JSON.stringify(payload))
  if (image) form.append('image', image, 'image')
  return form
}
