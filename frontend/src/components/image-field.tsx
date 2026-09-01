/**
 * Choosing a picture of an item, for the two forms that take one.
 *
 * The resize is the reason this is a component rather than an <input type="file">. The
 * services cap a stored image at 1 MiB, and a photograph off a phone is several times
 * that - so without this, the honest case (a customer photographing the thing they
 * want) would be the one that gets refused. Scaling happens here, where the file
 * already is, rather than being asked of the person choosing it.
 */

import { useEffect, useMemo, useRef, useState } from 'react'
import { ImagePlusIcon, XIcon } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

/** The longest edge a stored picture keeps. It is rendered into a card a few hundred
 *  pixels wide, so past this the bytes buy nothing a viewer can see. */
const MAX_EDGE = 1024

/** WebP at this quality holds a product photo well under the 1 MiB the services accept,
 *  and every browser that runs this app decodes it. */
const QUALITY = 0.82

/** What the picker offers. The services read the format from the bytes and accept the
 *  same three, but the canvas re-encodes to WebP regardless - so this list is about
 *  what a person may choose, not what is stored. */
const ACCEPT = 'image/jpeg,image/png,image/webp'

export interface ImageFieldProps {
  /** The chosen picture, already resized and re-encoded, or null. */
  value: Blob | null
  onChange: (image: Blob | null) => void
  /** Shown before anything is chosen - a picture the item already has, say. */
  initialPreviewUrl?: string | null
  disabled?: boolean
  id?: string
}

export function ImageField({
  value,
  onChange,
  initialPreviewUrl = null,
  disabled,
  id = 'image',
}: ImageFieldProps) {
  const input = useRef<HTMLInputElement>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // Derived from the blob rather than held in state: the preview is a function of what
  // is chosen, and storing it separately would be a second copy to keep in step.
  const preview = useMemo(() => (value ? URL.createObjectURL(value) : null), [value])

  // An object URL is a document-lifetime reference to a blob, so it has to be given
  // back when the blob is replaced - otherwise choosing five pictures in one dialog
  // leaks all five until the tab closes. Revoking is the only thing this effect does.
  useEffect(() => {
    if (!preview) return
    return () => URL.revokeObjectURL(preview)
  }, [preview])

  async function choose(file: File | undefined) {
    if (!file) return
    setError(null)
    setBusy(true)
    try {
      onChange(await downscale(file))
    } catch {
      // Decoding is where a file that is not really an image fails, whatever it is
      // named. The services check again on arrival; this only saves the round trip.
      setError('That file could not be read as an image. Try a JPEG, PNG or WebP.')
      onChange(null)
    } finally {
      setBusy(false)
      // Cleared so choosing the same file twice in a row still fires a change event.
      if (input.current) input.current.value = ''
    }
  }

  const shown = preview ?? initialPreviewUrl

  return (
    <div className="space-y-2">
      <input
        ref={input}
        id={id}
        type="file"
        accept={ACCEPT}
        className="sr-only"
        disabled={disabled || busy}
        onChange={(event) => void choose(event.target.files?.[0])}
      />

      {shown ? (
        <div className="relative w-fit">
          <img
            src={shown}
            alt="The picture chosen for this item"
            className="h-32 w-32 rounded-lg border object-cover"
          />
          {/* Removing clears the chosen file, not the item's existing picture: an
              upload only replaces one when a new file is actually sent. */}
          <Button
            type="button"
            size="icon"
            variant="secondary"
            className="absolute -top-2 -right-2 size-6 rounded-full shadow-sm"
            disabled={disabled || busy}
            onClick={() => {
              setError(null)
              onChange(null)
            }}
            aria-label="Remove this picture"
          >
            <XIcon className="size-3.5" />
          </Button>
        </div>
      ) : (
        <button
          type="button"
          disabled={disabled || busy}
          onClick={() => input.current?.click()}
          className={cn(
            'flex h-32 w-32 flex-col items-center justify-center gap-1.5 rounded-lg',
            'border border-dashed text-muted-foreground transition-colors',
            'hover:border-ring hover:text-foreground',
            'disabled:pointer-events-none disabled:opacity-50',
          )}
        >
          <ImagePlusIcon className="size-5" />
          <span className="text-xs">{busy ? 'Reading…' : 'Add a picture'}</span>
        </button>
      )}

      {error && <p className="text-xs text-destructive">{error}</p>}
    </div>
  )
}

/**
 * Scale a chosen file down to something worth storing, and re-encode it as WebP.
 *
 * Re-encoding rather than only resizing is what makes the size predictable: a PNG
 * photograph is enormous whatever its dimensions, and the cap the services enforce is
 * on bytes rather than pixels. It also drops whatever metadata the original carried,
 * which for a phone photo includes the GPS coordinates it was taken at - not something
 * anyone means to publish along with a picture of a kettle.
 */
async function downscale(file: File): Promise<Blob> {
  const bitmap = await createImageBitmap(file)
  try {
    const scale = Math.min(1, MAX_EDGE / Math.max(bitmap.width, bitmap.height))
    const canvas = document.createElement('canvas')
    canvas.width = Math.max(1, Math.round(bitmap.width * scale))
    canvas.height = Math.max(1, Math.round(bitmap.height * scale))

    const context = canvas.getContext('2d')
    if (!context) throw new Error('no 2d context')
    context.drawImage(bitmap, 0, 0, canvas.width, canvas.height)

    return await new Promise<Blob>((resolve, reject) => {
      canvas.toBlob(
        (blob) => (blob ? resolve(blob) : reject(new Error('encoding failed'))),
        'image/webp',
        QUALITY,
      )
    })
  } finally {
    // Frees the decoded pixels now rather than at the next collection, which for a
    // 12-megapixel photo is around 50 MB.
    bitmap.close()
  }
}
