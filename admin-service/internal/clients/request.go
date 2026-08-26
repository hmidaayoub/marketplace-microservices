package clients

import (
	"context"
	"net/http"

	"github.com/google/uuid"
)

type RequestClient struct{ t transport }

func NewRequest(baseURL, apiKey string, httpClient *http.Client) *RequestClient {
	return &RequestClient{t: transport{baseURL: baseURL, apiKey: apiKey, name: "request-service", http: httpClient}}
}

type participantsResponse struct {
	RequestID   uuid.UUID   `json:"requestId"`
	CustomerIDs []uuid.UUID `json:"customerIds"`
}

// SetStatus moves a request forward once an offer against it has been decided.
//
// Request-service accepts only forward transitions here: Admin/Contact decides offers,
// not demand, so it can mark a request as having an approved offer but cannot reopen one.
func (c *RequestClient) SetStatus(ctx context.Context, requestID uuid.UUID, status string) error {
	return c.t.do(ctx, http.MethodPatch,
		"/internal/requests/"+requestID.String()+"/status",
		map[string]string{"status": status}, nil)
}

// ParticipantCustomerIDs reads the customers behind a request. This is the endpoint
// request-service deliberately keeps off its public API - it is what turns an approved
// offer into a concrete set of people the seller may contact (flow 3, step 4).
func (c *RequestClient) ParticipantCustomerIDs(ctx context.Context, requestID uuid.UUID) ([]uuid.UUID, error) {
	var out participantsResponse
	if err := c.t.do(ctx, http.MethodGet,
		"/internal/requests/"+requestID.String()+"/participants", nil, &out); err != nil {
		return nil, err
	}
	return out.CustomerIDs, nil
}
