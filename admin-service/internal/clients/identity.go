package clients

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"
)

// The three hops that turn a token into a phone number (flow 4).
//
// A seller asks for the contacts on a request. The token gives a userId, which
// seller-service turns into the sellerId a grant is written against. Each granted
// customerId then goes to customer-service for its userId, and only that userId
// reaches auth-service's phone endpoint. Nothing short-circuits the chain: this
// service stores no phone number and no user identity of its own (R10).

type SellerClient struct{ t transport }

func NewSeller(baseURL, apiKey string, httpClient *http.Client) *SellerClient {
	return &SellerClient{t: transport{baseURL: baseURL, apiKey: apiKey, name: "seller-service", http: httpClient}}
}

// ResolveSellerID turns the token subject into the sellerId a ContactAccess row is
// keyed by. Identity is never taken from the caller's body or headers.
func (c *SellerClient) ResolveSellerID(ctx context.Context, userID uuid.UUID) (uuid.UUID, error) {
	var out struct {
		SellerID uuid.UUID `json:"sellerId"`
	}
	if err := c.t.do(ctx, http.MethodGet, "/internal/sellers/by-user/"+userID.String(), nil, &out); err != nil {
		return uuid.Nil, err
	}
	if out.SellerID == uuid.Nil {
		return uuid.Nil, fmt.Errorf("%w: seller-service: response had no sellerId", ErrUnavailable)
	}
	return out.SellerID, nil
}

// ResolveUserID maps a sellerId back to the global userId.
//
// The mirror of ResolveSellerID, needed because notification-service is addressed by
// userId and never resolves an identity itself: this service holds a sellerId on the
// offer it just decided, so it is the one that has to make the hop.
func (c *SellerClient) ResolveUserID(ctx context.Context, sellerID uuid.UUID) (uuid.UUID, error) {
	var out struct {
		UserID uuid.UUID `json:"userId"`
	}
	if err := c.t.do(ctx, http.MethodGet, "/internal/sellers/"+sellerID.String(), nil, &out); err != nil {
		return uuid.Nil, err
	}
	if out.UserID == uuid.Nil {
		return uuid.Nil, fmt.Errorf("%w: seller-service: response had no userId", ErrUnavailable)
	}
	return out.UserID, nil
}

type CustomerClient struct{ t transport }

func NewCustomer(baseURL, apiKey string, httpClient *http.Client) *CustomerClient {
	return &CustomerClient{t: transport{baseURL: baseURL, apiKey: apiKey, name: "customer-service", http: httpClient}}
}

// Customer is the part of a customer profile this service passes on: the join to the
// identity model, and the name a granted seller is shown alongside the number.
type Customer struct {
	UserID    uuid.UUID `json:"userId"`
	FirstName string    `json:"firstName"`
	LastName  string    `json:"lastName"`
}

// Resolve reads one customer profile. Request-service records participation as
// customerIds, but phone numbers are keyed by userId in auth-service, so this is the
// join between the two halves of the identity model - and the same response already
// carries the name, so putting a name next to a number costs no extra call.
func (c *CustomerClient) Resolve(ctx context.Context, customerID uuid.UUID) (Customer, error) {
	var out Customer
	if err := c.t.do(ctx, http.MethodGet, "/internal/customers/"+customerID.String(), nil, &out); err != nil {
		return Customer{}, err
	}
	if out.UserID == uuid.Nil {
		return Customer{}, fmt.Errorf("%w: customer-service: response had no userId", ErrUnavailable)
	}
	return out, nil
}

// ResolveUserID keeps the narrow call for the paths that only need the join.
func (c *CustomerClient) ResolveUserID(ctx context.Context, customerID uuid.UUID) (uuid.UUID, error) {
	customer, err := c.Resolve(ctx, customerID)
	if err != nil {
		return uuid.Nil, err
	}
	return customer.UserID, nil
}

type AuthClient struct{ t transport }

func NewAuth(baseURL, apiKey string, httpClient *http.Client) *AuthClient {
	return &AuthClient{t: transport{baseURL: baseURL, apiKey: apiKey, name: "auth-service", http: httpClient}}
}

// Phone fetches one phone number. This is the only call in the platform that returns
// one, and it is made only after a GRANTED ContactAccess row has been found for the
// seller asking (R9). The number is passed straight through to that seller and never
// written to admin_contact_db.
func (c *AuthClient) Phone(ctx context.Context, userID uuid.UUID) (string, error) {
	var out struct {
		PhoneNumber string `json:"phoneNumber"`
	}
	if err := c.t.do(ctx, http.MethodGet, "/internal/users/"+userID.String()+"/phone", nil, &out); err != nil {
		return "", err
	}
	return out.PhoneNumber, nil
}
