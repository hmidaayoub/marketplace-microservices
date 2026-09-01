// Package httpx holds the JSON response helpers shared by every handler.
package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"
)

// ErrorBody matches the error shape the Java services return, so a client sees one
// error contract across the platform rather than one per language.
type ErrorBody struct {
	Message string `json:"message"`
	Status  int    `json:"status"`
}

func JSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if payload == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		// The status line is already sent, so this can only be logged.
		slog.Error("writing response body", "error", err)
	}
}

func Error(w http.ResponseWriter, status int, message string) {
	JSON(w, status, ErrorBody{Message: message, Status: status})
}

// DecodeJSON reads a request body strictly: unknown fields are rejected so a caller
// cannot smuggle in a field the handler silently ignores (for example a customerId
// that identity must come from the token instead).
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var syntax *json.SyntaxError
		switch {
		case errors.As(err, &syntax):
			Error(w, http.StatusBadRequest, "Malformed JSON body")
		case errors.Is(err, io.EOF):
			Error(w, http.StatusBadRequest, "Request body is required")
		default:
			Error(w, http.StatusBadRequest, "Invalid request body")
		}
		return false
	}
	return true
}

// DecodeJSONOrMultipart reads a body that may carry a file alongside its JSON.
//
// Two content types, one handler. A plain application/json body is decoded exactly as
// DecodeJSON would - which is what keeps every existing caller working untouched: the
// smoke script, the Postman collection and offer-service's internal calls all still
// send the JSON they always sent, and none of them had to learn about images.
//
// A multipart/form-data body carries the same JSON in a part named "payload", plus an
// optional file part named "image". The JSON stays in one part rather than being spread
// across form fields because the bodies here are not flat - an offer names the item it
// is for as a nested object - and flattening a structure into form keys would mean a
// second, hand-written parser that could disagree with the first about what is valid.
//
// Returns the raw image bytes, which are nil when no file part was sent. Their format
// is not decided here: media.Detect reads the bytes themselves, because the part's
// declared Content-Type is a claim by the uploader.
func DecodeJSONOrMultipart(
	w http.ResponseWriter, r *http.Request, dst any, maxImageBytes int64,
) (image []byte, ok bool) {
	kind, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || kind != "multipart/form-data" {
		return nil, DecodeJSON(w, r, dst)
	}

	// The JSON part is small; only the file part needs the larger allowance, and it is
	// bounded so a body that lies about its size is cut off rather than buffered.
	if err := r.ParseMultipartForm(maxImageBytes + 1<<20); err != nil {
		Error(w, http.StatusBadRequest, "Malformed multipart body")
		return nil, false
	}

	payload := r.FormValue("payload")
	if payload == "" {
		Error(w, http.StatusBadRequest, `Multipart body needs a "payload" part carrying the JSON fields`)
		return nil, false
	}
	dec := json.NewDecoder(strings.NewReader(payload))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		Error(w, http.StatusBadRequest, "Invalid JSON in the payload part")
		return nil, false
	}

	file, _, err := r.FormFile("image")
	if errors.Is(err, http.ErrMissingFile) {
		// A form submitted with no picture chosen. The rest of the body still stands.
		return nil, true
	}
	if err != nil {
		Error(w, http.StatusBadRequest, "Could not read the image part")
		return nil, false
	}
	defer func() { _ = file.Close() }()

	// One byte past the cap, so a file that is exactly too big is still seen to be too
	// big rather than silently truncated to the limit and stored as valid.
	data, err := io.ReadAll(io.LimitReader(file, maxImageBytes+1))
	if err != nil {
		Error(w, http.StatusBadRequest, "Could not read the image part")
		return nil, false
	}
	return data, true
}
