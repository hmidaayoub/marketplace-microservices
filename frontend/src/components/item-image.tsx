/**
 * The picture of what a request asks for, and the picture of what an offer sells.
 *
 * Two components rather than one, because the two are not fetched the same way and the
 * difference is not cosmetic - it is the projection. A request is public, so its
 * picture is a URL an <img> can point at. An offer is not: a seller may see their own
 * but never a rival's, which means a token, and a token means the browser cannot fetch
 * it as an image at all. See api/images, which holds the reasoning and the fetching;
 * this file is only the two ways of putting the result on a page.
 *
 * Both render nothing when there is no picture. That is what keeps a list of mostly
 * unillustrated demand a list rather than a column of grey placeholder boxes.
 */

import { requestImageUrl, useOfferImage } from '@/api/images'
import { cn } from '@/lib/utils'

/** Sized by the caller - a thumbnail in a row, a banner on a detail page - so only the
 *  frame the two share is set here. */
const FRAME = 'rounded-lg border object-cover'

export function RequestImage({
  requestId,
  hasImage,
  className,
}: {
  requestId: string | undefined
  /** Read off the request. The service sends it precisely so a client can decide
   *  whether to render this without asking the image endpoint first. */
  hasImage: boolean | undefined
  className?: string
}) {
  if (!requestId || !hasImage) return null
  return (
    <img
      src={requestImageUrl(requestId)}
      alt=""
      // Lists of these can be long, and a picture below the fold is one nobody has
      // scrolled to yet.
      loading="lazy"
      className={cn(FRAME, className)}
    />
  )
}

export function OfferImage({
  offerId,
  hasImage,
  version,
  className,
}: {
  offerId: string | undefined
  /** Present on the full offer a customer, an admin or the offer's own seller reads.
   *  A rival seller's projection does not carry it, so their picture is never even
   *  asked for - which is the same answer the endpoint would give them anyway. */
  hasImage: boolean | undefined
  /** The offer's updatedAt, where the caller has it. A picture is replaced as a whole,
   *  so the offer's own version is the picture's - see useOfferImage. */
  version?: string
  className?: string
}) {
  // Unconditional, because hooks are: the flag decides whether it fetches, not whether
  // it runs.
  const url = useOfferImage(offerId, Boolean(hasImage), version)
  if (!url) return null
  return <img src={url} alt="" className={cn(FRAME, className)} />
}
