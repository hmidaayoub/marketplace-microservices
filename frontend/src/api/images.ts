/**
 * The two image endpoints, which are not symmetrical - and the asymmetry is the whole
 * content of this file.
 *
 * A request's picture is public, like the request itself, so it is just a URL an <img>
 * can point at. An offer's picture is not: it obeys the same projection as the offer,
 * where a seller may see their own but never a competitor's. That means a token, and a
 * token means an <img src> cannot fetch it - the browser sends cookies with an image
 * request, never an Authorization header. So an offer's picture has to be fetched the
 * way every other authenticated call is and turned into an object URL.
 */

import { useEffect, useState } from 'react'

import { api } from '@/api/client'
import { useAppSelector } from '@/store'

const BASE = import.meta.env.VITE_API_URL ?? 'http://localhost:8080'

/**
 * Where a request's picture lives. Safe to put straight in an <img src>: the endpoint
 * is open, for the same reason browsing demand is.
 *
 * Built from the id rather than read off a field the service sends, so there is one
 * routing table and not two - the response carries `hasImage`, which is the part a
 * client cannot work out for itself.
 */
export function requestImageUrl(requestId: string): string {
  return `${BASE}/api/requests/${requestId}/image`
}

/**
 * An offer's picture, fetched with the caller's token and handed back as an object URL.
 *
 * Returns null while it is loading and if it fails - a 404 for an offer that carries
 * none, or the deliberate 404 a rival seller gets - so the caller renders nothing
 * rather than a broken image.
 */
export function useOfferImage(
  offerId: string | undefined,
  hasImage: boolean,
  /**
   * The offer's updatedAt, when the caller has it.
   *
   * Carried into the URL rather than only into the dependencies, because the staleness
   * to beat is the browser's and not React's: the response is cacheable for five
   * minutes, so a seller who replaces their picture would keep being served the old one
   * from cache however many times this refetched. A version in the query string makes
   * the new picture a new URL. The service ignores the parameter - the id in the path
   * is what it reads - and the ETag it answers with is built from this same timestamp.
   */
  version?: string,
): string | null {
  const token = useAppSelector((s) => s.auth.accessToken)
  // Keyed by what it was fetched for, so a stale URL is never shown against a new
  // offer: the key is compared during render rather than cleared by an effect.
  const [fetched, setFetched] = useState<{ key: string; url: string } | null>(null)

  const wanted = offerId && hasImage && token ? offerId : null
  const key = wanted ? `${wanted}@${version ?? ''}` : null

  useEffect(() => {
    if (!wanted || !key) return

    // Guards the two races an async fetch into state has: the component unmounting
    // before it lands, and a second offerId overtaking the first.
    let live = true
    let created: string | null = null

    const query = version ? `?v=${encodeURIComponent(version)}` : ''
    void api<Blob>(`/api/offers/${wanted}/image${query}`, { token, responseType: 'blob' })
      .then((blob) => {
        if (!live) return
        created = URL.createObjectURL(blob)
        setFetched({ key, url: created })
      })
      .catch(() => {
        // A 404 for an offer that carries none, or the deliberate 404 a rival seller
        // gets. Either way there is nothing to show, which is the initial state.
      })

    return () => {
      live = false
      // The object URL is a document-lifetime reference to the blob; without this the
      // bytes stay resident for as long as the tab does.
      if (created) URL.revokeObjectURL(created)
    }
  }, [wanted, key, version, token])

  return fetched && fetched.key === key ? fetched.url : null
}
