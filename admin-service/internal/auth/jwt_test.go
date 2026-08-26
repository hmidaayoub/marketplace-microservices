package auth

import (
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

// The dev secret from docker-compose.yml. It is 48 bytes, which is what makes jjwt
// choose HS384 - see the golden tokens below.
const devSecret = "dev-secret-key-change-in-production-min-256-bits"

const wantUserID = "11111111-1111-1111-1111-111111111111"

// These three tokens were minted by the real Java stack: common-security's jjwt, the
// dev secret above, through the same builder calls JwtUtil makes. They are golden
// values rather than Go-generated lookalikes precisely because the thing under test is
// cross-language compatibility - a Go-minted token would hide any jjwt-specific
// behaviour. The valid ones expire in 2035.
const (
	javaAccessToken  = "eyJhbGciOiJIUzM4NCJ9.eyJzdWIiOiIxMTExMTExMS0xMTExLTExMTEtMTExMS0xMTExMTExMTExMTEiLCJlbWFpbCI6ImludGVyb3BAdGVzdC5jb20iLCJyb2xlIjoiQ1VTVE9NRVIiLCJpYXQiOjE3ODc2NzUyNjcsImV4cCI6MjEwMzAzNTI2N30.WJd6ItkA32ET7iND4BS520bFW0I7B6hW3Oql4c-Ndkwd8sS7A4m9MZ9SpbomtHLh"
	javaRefreshToken = "eyJhbGciOiJIUzM4NCJ9.eyJzdWIiOiIxMTExMTExMS0xMTExLTExMTEtMTExMS0xMTExMTExMTExMTEiLCJqdGkiOiI1ZmJiMDkwYS0yNDJiLTRjZjMtOTI4Ni0xYTA1ZjgxMGZkMDEiLCJ0eXBlIjoicmVmcmVzaCIsImlhdCI6MTc4NzY3NTI2NywiZXhwIjoyMTAzMDM1MjY3fQ.f50LI5fSM7Dbng6nbHwwwENyxHjIzFBO99gVdXdk4jNMEkNfA2e771NhFSoM-JHf"
	javaExpiredToken = "eyJhbGciOiJIUzM4NCJ9.eyJzdWIiOiIxMTExMTExMS0xMTExLTExMTEtMTExMS0xMTExMTExMTExMTEiLCJyb2xlIjoiQ1VTVE9NRVIiLCJpYXQiOjE3ODc2NjgwNjcsImV4cCI6MTc4NzY3MTY2N30.HlBEQ0_9jcCTSAFPj83Eiv---At9-H4UdAjtTAYFkMwhO0PVAcDRTw0DR8wp3jl1"
)

// This is the regression that matters: jjwt's signWith(SecretKey) selects the HMAC
// variant from the key size, so the 384-bit dev secret yields HS384. A verifier pinned
// to HS256 compiles, passes any Go-only test, and then rejects every token in
// production. Assert the header explicitly so the reason stays visible.
func TestJavaTokensAreHS384(t *testing.T) {
	header := strings.Split(javaAccessToken, ".")[0]
	decoded, err := jwt.NewParser().DecodeSegment(header)
	if err != nil {
		t.Fatalf("decoding JOSE header: %v", err)
	}
	if got := string(decoded); got != `{"alg":"HS384"}` {
		t.Fatalf("expected the Java token to be HS384, got %s", got)
	}
}

func TestVerifyAcceptsRealJavaAccessToken(t *testing.T) {
	claims, err := NewVerifier([]byte(devSecret)).Verify(javaAccessToken)
	if err != nil {
		t.Fatalf("a token minted by auth-service must verify, got: %v", err)
	}
	if claims.UserID.String() != wantUserID {
		t.Errorf("UserID = %s, want %s", claims.UserID, wantUserID)
	}
	if claims.Role != RoleCustomer {
		t.Errorf("Role = %s, want %s", claims.Role, RoleCustomer)
	}
	if claims.Email != "interop@test.com" {
		t.Errorf("Email = %s, want interop@test.com", claims.Email)
	}
}

// A refresh token is signed with the same key and would otherwise sail through
// signature verification. It must not authenticate an API call.
func TestVerifyRejectsRefreshToken(t *testing.T) {
	if _, err := NewVerifier([]byte(devSecret)).Verify(javaRefreshToken); err == nil {
		t.Fatal("expected a refresh token to be rejected")
	}
}

func TestVerifyRejectsExpiredToken(t *testing.T) {
	if _, err := NewVerifier([]byte(devSecret)).Verify(javaExpiredToken); err == nil {
		t.Fatal("expected an expired token to be rejected")
	}
}

func TestVerifyRejectsWrongSecret(t *testing.T) {
	if _, err := NewVerifier([]byte("a-different-secret-of-sufficient-length-here")).Verify(javaAccessToken); err == nil {
		t.Fatal("expected a token signed with another secret to be rejected")
	}
}

// The classic algorithm-confusion attack: strip the signature and claim "none".
func TestVerifyRejectsNoneAlgorithm(t *testing.T) {
	token := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{
		"sub": wantUserID, "role": RoleCustomer,
	})
	signed, err := token.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("building unsigned token: %v", err)
	}
	if _, err := NewVerifier([]byte(devSecret)).Verify(signed); err == nil {
		t.Fatal("expected an alg=none token to be rejected")
	}
}

func TestVerifyRejectsTokenWithoutRole(t *testing.T) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS384, jwt.MapClaims{
		"sub": wantUserID, "exp": 4102444800,
	})
	signed, err := token.SignedString([]byte(devSecret))
	if err != nil {
		t.Fatalf("signing: %v", err)
	}
	if _, err := NewVerifier([]byte(devSecret)).Verify(signed); err == nil {
		t.Fatal("expected a token with no role claim to be rejected")
	}
}

func TestVerifyRejectsNonUUIDSubject(t *testing.T) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS384, jwt.MapClaims{
		"sub": "not-a-uuid", "role": RoleCustomer, "exp": 4102444800,
	})
	signed, err := token.SignedString([]byte(devSecret))
	if err != nil {
		t.Fatalf("signing: %v", err)
	}
	if _, err := NewVerifier([]byte(devSecret)).Verify(signed); err == nil {
		t.Fatal("expected a non-uuid subject to be rejected")
	}
}
