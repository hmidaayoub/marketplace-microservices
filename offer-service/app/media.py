"""The rules for the one kind of file the platform accepts: a picture of a product.

The Go twin of this module is request-service's internal/media. The two services hold
the same rules because they hold the same kind of file, and a picture the platform would
refuse on a request is not one it should accept on an offer.
"""

# Caps a stored image. Chosen against what it is for - a photo rendered into a card a
# few hundred pixels wide - rather than against what a camera produces, and the browser
# resizes to roughly a tenth of this before uploading. It is the last line rather than
# the first: the gateway caps the body before the request reaches any service, and the
# database has a CHECK above it again.
MAX_BYTES = 1 << 20  # 1 MiB

# The formats a browser renders without a plugin, a codec or a polyfill, keyed by the
# magic bytes that identify them.
#
# SVG is deliberately absent. It is a document, not a bitmap: it can carry script, and
# serving one back from the platform's own origin would hand an uploader a stored XSS
# against every viewer of the page. There is no sanitiser here that would make it safe,
# so it is not accepted at all.
_SIGNATURES: tuple[tuple[bytes, str], ...] = (
    (b"\xff\xd8\xff", "image/jpeg"),
    (b"\x89PNG\r\n\x1a\n", "image/png"),
)

ALLOWED = ("image/jpeg", "image/png", "image/webp")


class UnsupportedImage(ValueError):
    """The bytes are not one of the formats the platform serves."""


def detect(data: bytes) -> str:
    """Return the media type of the bytes themselves.

    The upload's declared Content-Type is never consulted, and that is the point. A
    declared type is a claim by the uploader, and the response that serves this image
    back sets its Content-Type from what is stored - so believing the claim would let
    somebody upload HTML labelled image/png and have the platform serve it back as HTML
    from its own origin. Reading the signature means the label always describes the
    bytes.
    """
    if not data:
        raise UnsupportedImage("image is empty")
    if len(data) > MAX_BYTES:
        raise UnsupportedImage(f"image must be at most {MAX_BYTES} bytes")

    for signature, media_type in _SIGNATURES:
        if data.startswith(signature):
            return media_type

    # WebP is a RIFF container: "RIFF", four bytes of length, then "WEBP". The length
    # in between is why this one cannot be a simple prefix match.
    if data[:4] == b"RIFF" and data[8:12] == b"WEBP":
        return "image/webp"

    raise UnsupportedImage("unsupported image format (accepted: JPEG, PNG, WebP)")
