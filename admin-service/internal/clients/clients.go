// Package clients holds outbound calls to the other services.
//
// Admin/Contact is the most connected service in the platform: deciding an offer
// touches offer-service and request-service, and answering a seller's contact lookup
// walks seller-service, customer-service and auth-service in turn. Every call goes to
// an /internal endpoint carrying the shared key - this service never forwards a user's
// token, so a dependency can never be reached with more authority than the platform
// grants Admin/Contact itself.
package clients

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/hmidaayoub/marketplace-microservices/admin-service/internal/middleware"
)

var (
	// ErrNotFound is a 404 from a dependency: the thing genuinely is not there, which
	// is an answer rather than a fault, and usually becomes a 404 to our own caller.
	ErrNotFound = errors.New("not found")

	// ErrConflict is a 409 from a dependency, which means the state it owns does not
	// allow the change - an offer that has already been decided, for instance.
	ErrConflict = errors.New("rejected as a conflict")

	// ErrUnavailable covers a transport failure or an unexpected status. It maps to
	// 503, matching how the Java services report a dependency being down.
	ErrUnavailable = errors.New("dependency unavailable")
)

// transport performs one internal call and decodes the JSON result.
type transport struct {
	baseURL string
	apiKey  string
	name    string
	http    *http.Client
}

func (t transport) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		encoded, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("%w: %s: encoding body: %v", ErrUnavailable, t.name, err)
		}
		body = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, t.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrUnavailable, t.name, err)
	}
	req.Header.Set(middleware.InternalAPIKeyHeader, t.apiKey)
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := t.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrUnavailable, t.name, err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated, http.StatusNoContent:
	case http.StatusNotFound:
		return fmt.Errorf("%w: %s", ErrNotFound, t.name)
	case http.StatusConflict:
		return fmt.Errorf("%w: %s", ErrConflict, t.name)
	default:
		return fmt.Errorf("%w: %s: status %d", ErrUnavailable, t.name, resp.StatusCode)
	}

	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%w: %s: decoding response: %v", ErrUnavailable, t.name, err)
	}
	return nil
}
