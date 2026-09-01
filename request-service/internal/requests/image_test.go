package requests

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A one-pixel PNG. Real bytes rather than a stub, because what the handler stores is
// decided by sniffing them - a placeholder would be refused, which is the point.
var onePixelPNG = []byte{
	0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 'I', 'H', 'D', 'R',
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89,
	0x00, 0x00, 0x00, 0x0a, 'I', 'D', 'A', 'T',
	0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00, 0x05, 0x00, 0x01,
	0x0d, 0x0a, 0x2d, 0xb4,
	0x00, 0x00, 0x00, 0x00, 'I', 'E', 'N', 'D', 0xae, 0x42, 0x60, 0x82,
}

// doMultipart posts a create the way the browser form does: the JSON in a payload part,
// the file beside it.
func (h *harness) doMultipart(path, token, payload string, filename string, image []byte) response {
	h.t.Helper()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if err := w.WriteField("payload", payload); err != nil {
		h.t.Fatalf("writing payload part: %v", err)
	}
	if image != nil {
		part, err := w.CreateFormFile("image", filename)
		if err != nil {
			h.t.Fatalf("creating file part: %v", err)
		}
		if _, err := part.Write(image); err != nil {
			h.t.Fatalf("writing file part: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		h.t.Fatalf("closing multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, path, &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)

	out := response{code: rec.Code, raw: rec.Body.String()}
	if trimmed := strings.TrimSpace(out.raw); strings.HasPrefix(trimmed, "{") {
		_ = json.Unmarshal([]byte(trimmed), &out.body)
	}
	return out
}

// raw fetches a path and hands back the recorder, for the responses that are not JSON.
func (h *harness) raw(path string, headers map[string]string) *httptest.ResponseRecorder {
	h.t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, strings.NewReader(""))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	return rec
}

func TestCreateRequest_storesAndServesAnImage(t *testing.T) {
	h := newHarness(t)
	_, token := h.newCustomer()

	res := h.doMultipart("/api/requests", token,
		`{"itemName":"Espresso Machine","description":"bar grade","category":"kitchen","quantity":3}`,
		"machine.png", onePixelPNG)

	if res.code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body %s", res.code, res.raw)
	}
	// The rest of the body is unaffected by the picture travelling beside it.
	if got := res.body["itemName"]; got != "Espresso Machine" {
		t.Errorf("itemName = %v", got)
	}
	if got := num(t, res.body, "totalQuantity"); got != 3 {
		t.Errorf("totalQuantity = %d, want 3", got)
	}
	if res.body["hasImage"] != true {
		t.Errorf("hasImage = %v, want true", res.body["hasImage"])
	}

	requestID := res.body["requestId"].(string)
	rec := h.raw("/api/requests/"+requestID+"/image", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("image status = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", got)
	}
	// Sniffed, not believed: the part above was named .png and the bytes agreed, but
	// the header has to come from the bytes either way.
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if !bytes.Equal(rec.Body.Bytes(), onePixelPNG) {
		t.Errorf("served %d bytes, want the %d stored", rec.Body.Len(), len(onePixelPNG))
	}
}

// The picture is optional, and its absence must not change anything else about a create.
func TestCreateRequest_withoutAnImageReportsNone(t *testing.T) {
	h := newHarness(t)
	_, token := h.newCustomer()

	res := h.doMultipart("/api/requests", token,
		`{"itemName":"Kettle","description":"","category":"","quantity":1}`, "", nil)

	if res.code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body %s", res.code, res.raw)
	}
	if res.body["hasImage"] != false {
		t.Errorf("hasImage = %v, want false", res.body["hasImage"])
	}

	rec := h.raw("/api/requests/"+res.body["requestId"].(string)+"/image", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("image status = %d, want 404", rec.Code)
	}
}

// The JSON path is the one every existing caller uses - the smoke script, the Postman
// collection, offer-service. Adding images must not have moved it.
func TestCreateRequest_plainJSONStillWorks(t *testing.T) {
	h := newHarness(t)
	_, token := h.newCustomer()

	res := h.do(http.MethodPost, "/api/requests", token,
		`{"itemName":"Grinder","description":"d","category":"c","quantity":2}`)

	if res.code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body %s", res.code, res.raw)
	}
	if res.body["hasImage"] != false {
		t.Errorf("hasImage = %v, want false", res.body["hasImage"])
	}
}

// Storing markup and serving it back from the platform's own origin would be a stored
// XSS against every viewer. The filename and the part's Content-Type both claim PNG;
// only the bytes are consulted.
func TestCreateRequest_refusesAFileThatIsNotAnImage(t *testing.T) {
	h := newHarness(t)
	_, token := h.newCustomer()

	res := h.doMultipart("/api/requests", token,
		`{"itemName":"Trojan","description":"","category":"","quantity":1}`,
		"innocent.png", []byte("<html><script>alert(1)</script></html>"))

	if res.code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body %s", res.code, res.raw)
	}
	if msg, _ := res.body["message"].(string); !strings.Contains(msg, "unsupported image format") {
		t.Errorf("message = %q", msg)
	}
}

func TestCreateRequest_refusesAnOversizedImage(t *testing.T) {
	h := newHarness(t)
	_, token := h.newCustomer()

	// A real PNG header with more than the cap behind it, so it is the size that is
	// refused rather than the format.
	huge := append(append([]byte{}, onePixelPNG...), bytes.Repeat([]byte{0}, 1<<20)...)

	res := h.doMultipart("/api/requests", token,
		`{"itemName":"Huge","description":"","category":"","quantity":1}`, "huge.png", huge)

	if res.code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body %s", res.code, res.raw)
	}
}

// A refused create leaves nothing behind - including no orphaned image row, since the
// picture is written inside the same transaction as the request.
func TestCreateRequest_refusedByDuplicateItemStoresNoImage(t *testing.T) {
	h := newHarness(t)
	_, first := h.newCustomer()
	_, second := h.newCustomer()

	h.createRequest(first, "Duplicated Item", 1)

	res := h.doMultipart("/api/requests", second,
		`{"itemName":"Duplicated Item","description":"","category":"","quantity":1}`,
		"x.png", onePixelPNG)

	if res.code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body %s", res.code, res.raw)
	}

	var images int
	if err := testPool.QueryRow(h.t.Context(),
		`SELECT count(*) FROM request_image`).Scan(&images); err != nil {
		t.Fatalf("counting images: %v", err)
	}
	if images != 0 {
		t.Errorf("request_image has %d rows, want 0 - the refused create wrote one", images)
	}
}

// A picture is replaced as a whole or not at all, so the stored timestamp is a complete
// validator and the browser need not re-download on every render.
func TestRequestImage_answersNotModifiedForAKnownETag(t *testing.T) {
	h := newHarness(t)
	_, token := h.newCustomer()

	res := h.doMultipart("/api/requests", token,
		`{"itemName":"Cached Item","description":"","category":"","quantity":1}`,
		"c.png", onePixelPNG)
	if res.code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body %s", res.code, res.raw)
	}
	path := "/api/requests/" + res.body["requestId"].(string) + "/image"

	first := h.raw(path, nil)
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("no ETag on the first response")
	}

	second := h.raw(path, map[string]string{"If-None-Match": etag})
	if second.Code != http.StatusNotModified {
		t.Errorf("status = %d, want 304", second.Code)
	}
	if second.Body.Len() != 0 {
		t.Errorf("304 carried %d bytes of body", second.Body.Len())
	}
}

// Reading demand needs no token, and neither does the picture attached to it: a visitor
// browses before being asked to sign in.
func TestRequestImage_isReadableWithoutAToken(t *testing.T) {
	h := newHarness(t)
	_, token := h.newCustomer()

	res := h.doMultipart("/api/requests", token,
		`{"itemName":"Public Item","description":"","category":"","quantity":1}`,
		"p.png", onePixelPNG)
	if res.code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body %s", res.code, res.raw)
	}

	rec := h.raw("/api/requests/"+res.body["requestId"].(string)+"/image", nil)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 with no Authorization header", rec.Code)
	}
}
