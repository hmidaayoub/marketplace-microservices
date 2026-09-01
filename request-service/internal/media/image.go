// Package media holds the rules for the one kind of file the platform accepts: a
// picture of the item a request is for.
package media

import (
	"errors"
	"fmt"
	"net/http"
)

// MaxBytes caps a stored image. Chosen against what it is for - a photo of a product,
// rendered into a card a few hundred pixels wide - rather than against what a camera
// produces, and the browser resizes to roughly a tenth of this before uploading. It is
// the last line rather than the first: the gateway caps the body before the request
// reaches any service, and the database has a CHECK above it again.
const MaxBytes = 1 << 20 // 1 MiB

// Allowed is the set a browser renders without a plugin, a codec or a polyfill.
//
// SVG is deliberately absent. It is a document, not a bitmap: it can carry script, and
// serving one from the platform's own origin would hand an uploader a stored XSS
// against every viewer of the page. There is no sanitiser here that would make it safe,
// so it is not accepted at all.
var Allowed = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/webp": true,
}

var (
	// ErrEmpty is a body with nothing in it - a form submitted with no file chosen.
	ErrEmpty  = errors.New("image is empty")
	ErrTooBig = fmt.Errorf("image must be at most %d bytes", MaxBytes)
)

// Detect returns the media type of the bytes themselves.
//
// The client's Content-Type is not consulted, and that is the point. A declared type is
// a claim by the uploader, and the response that serves this image back later sets its
// Content-Type from what is stored - so believing the claim would let somebody upload
// HTML labelled image/png and have the platform serve it back as HTML from its own
// origin. Sniffing means the label always describes the bytes.
func Detect(data []byte) (string, error) {
	switch {
	case len(data) == 0:
		return "", ErrEmpty
	case len(data) > MaxBytes:
		return "", ErrTooBig
	}

	// DetectContentType reads at most the first 512 bytes and never fails; it returns
	// application/octet-stream for anything it does not recognise.
	kind := http.DetectContentType(data)
	if !Allowed[kind] {
		return "", fmt.Errorf("unsupported image format: %s (accepted: JPEG, PNG, WebP)", kind)
	}
	return kind, nil
}
