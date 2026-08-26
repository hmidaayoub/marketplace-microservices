// Package auth verifies the access tokens minted by auth-service.
//
// The tokens come from jjwt (common-security/JwtUtil). Two details of that
// implementation drive the code here:
//
//  1. jjwt's signWith(SecretKey) picks the HMAC variant from the key length: a
//     384-bit secret yields HS384, not HS256. The deployed dev secret is exactly 48
//     bytes, so pinning this verifier to HS256 would reject every real token. We
//     accept the HMAC family instead and reject everything else, which still closes
//     the algorithm-confusion hole (an attacker cannot present "none", nor an RS256
//     token whose "signature" is our secret used as a public key).
//
//  2. Refresh tokens are signed with the same key and differ only by a "type":
//     "refresh" claim. They must not authenticate an API call, so they are rejected.
package auth

import (
	"errors"
	"fmt"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

var ErrInvalidToken = errors.New("invalid token")

// Claims is the authenticated identity taken from the token. Identity always comes
// from "sub"; no handler may read it from a header or body.
type Claims struct {
	UserID uuid.UUID
	Email  string
	Role   string
}

const (
	RoleCustomer = "CUSTOMER"
	RoleSeller   = "SELLER"
	RoleAdmin    = "ADMIN"
)

type Verifier struct {
	secret []byte
	parser *jwt.Parser
}

func NewVerifier(secret []byte) *Verifier {
	return &Verifier{
		secret: secret,
		parser: jwt.NewParser(
			jwt.WithValidMethods([]string{"HS256", "HS384", "HS512"}),
			jwt.WithExpirationRequired(),
		),
	}
}

func (v *Verifier) Verify(token string) (Claims, error) {
	parsed, err := v.parser.Parse(token, func(t *jwt.Token) (any, error) {
		return v.secret, nil
	})
	if err != nil {
		return Claims{}, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}

	mapClaims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return Claims{}, ErrInvalidToken
	}

	if typ, _ := mapClaims["type"].(string); typ == "refresh" {
		return Claims{}, fmt.Errorf("%w: refresh token cannot authenticate a request", ErrInvalidToken)
	}

	subject, err := mapClaims.GetSubject()
	if err != nil || subject == "" {
		return Claims{}, fmt.Errorf("%w: missing subject", ErrInvalidToken)
	}
	userID, err := uuid.Parse(subject)
	if err != nil {
		return Claims{}, fmt.Errorf("%w: subject is not a uuid", ErrInvalidToken)
	}

	role, _ := mapClaims["role"].(string)
	if role == "" {
		return Claims{}, fmt.Errorf("%w: missing role", ErrInvalidToken)
	}
	email, _ := mapClaims["email"].(string)

	return Claims{UserID: userID, Email: email, Role: role}, nil
}
