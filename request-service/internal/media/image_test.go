package media

import (
	"bytes"
	"strings"
	"testing"
)

// The first bytes of each format, which is all DetectContentType reads.
var (
	pngHeader  = []byte("\x89PNG\r\n\x1a\n")
	jpegHeader = []byte("\xff\xd8\xff\xe0")
	webpHeader = []byte("RIFF\x00\x00\x00\x00WEBPVP8 ")
)

func TestDetectAcceptsTheThreeRenderableFormats(t *testing.T) {
	for name, header := range map[string][]byte{
		"image/png":  pngHeader,
		"image/jpeg": jpegHeader,
		"image/webp": webpHeader,
	} {
		got, err := Detect(append(header, bytes.Repeat([]byte{0}, 64)...))
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", name, err)
		}
		if got != name {
			t.Fatalf("%s: got %s", name, got)
		}
	}
}

// The whole reason Detect exists: a caller's own Content-Type is never consulted, so
// markup cannot be stored as an image and served back from the platform's origin.
func TestDetectRejectsMarkupWhateverItClaimsToBe(t *testing.T) {
	_, err := Detect([]byte("<html><script>alert(1)</script></html>"))
	if err == nil {
		t.Fatal("expected HTML to be refused")
	}
	if !strings.Contains(err.Error(), "unsupported image format") {
		t.Fatalf("got %v", err)
	}
}

// SVG is markup too, and DetectContentType reports it as text/xml or text/plain -
// either way it is not in Allowed, which is what keeps a scriptable document out.
func TestDetectRejectsSVG(t *testing.T) {
	svg := []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"></svg>`)
	if _, err := Detect(svg); err == nil {
		t.Fatal("expected SVG to be refused")
	}
}

func TestDetectRejectsEmptyAndOversized(t *testing.T) {
	if _, err := Detect(nil); err != ErrEmpty {
		t.Fatalf("empty: got %v", err)
	}
	huge := append(pngHeader, bytes.Repeat([]byte{0}, MaxBytes)...)
	if _, err := Detect(huge); err != ErrTooBig {
		t.Fatalf("oversized: got %v", err)
	}
}
