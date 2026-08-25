// Package clients holds outbound calls to other services.
package clients

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/hmidaayoub/marketplace-microservices/request-service/internal/middleware"
)

// ErrCustomerNotFound means the authenticated user has no customer profile yet, so
// they cannot take part in a request. It is a 404 from customer-service, not a fault.
var ErrCustomerNotFound = errors.New("customer profile not found")

// ErrCustomerServiceUnavailable covers a transport failure or an unexpected status.
// It maps to 503, matching how the Java services report a dependency being down.
var ErrCustomerServiceUnavailable = errors.New("customer-service unavailable")

// Customer resolves the customerId behind a userId.
//
// The identity model (spec section 5) keeps userId global and customerId local to
// customer-service; RequestParticipant stores customerId. This service therefore never
// accepts a customerId from a caller - it always derives one from the token subject.
type Customer struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

func NewCustomer(baseURL, apiKey string, httpClient *http.Client) *Customer {
	return &Customer{baseURL: baseURL, apiKey: apiKey, http: httpClient}
}

type customerResponse struct {
	CustomerID uuid.UUID `json:"customerId"`
}

func (c *Customer) ResolveCustomerID(ctx context.Context, userID uuid.UUID) (uuid.UUID, error) {
	url := fmt.Sprintf("%s/internal/customers/by-user/%s", c.baseURL, userID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: %v", ErrCustomerServiceUnavailable, err)
	}
	req.Header.Set(middleware.InternalAPIKeyHeader, c.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: %v", ErrCustomerServiceUnavailable, err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return uuid.Nil, ErrCustomerNotFound
	default:
		return uuid.Nil, fmt.Errorf("%w: status %d", ErrCustomerServiceUnavailable, resp.StatusCode)
	}

	var body customerResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return uuid.Nil, fmt.Errorf("%w: decoding response: %v", ErrCustomerServiceUnavailable, err)
	}
	if body.CustomerID == uuid.Nil {
		return uuid.Nil, fmt.Errorf("%w: response had no customerId", ErrCustomerServiceUnavailable)
	}
	return body.CustomerID, nil
}
