package handlers_test

import (
	"crypto/rsa"
	"dbBackend/handlers"
	"dbBackend/internal/testutil"
	"dbBackend/models"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// getTestKeys loads and returns the RSA key pair for testing handlers.
func getTestKeys(t *testing.T) (*rsa.PrivateKey, *rsa.PublicKey) {
	t.Helper()
	privateKey, err := handlers.GetPrivateKey()
	if err != nil {
		t.Fatalf("failed to read private key for test: %v", err)
	}
	publicKey, err := handlers.GetPublicKey()
	if err != nil {
		t.Fatalf("failed to read public key for test: %v", err)
	}
	return privateKey, publicKey
}

// makeAuthHeader creates a valid signed RS256 JWT Bearer Authorization header for a test user.
func makeAuthHeader(t *testing.T, user *models.User, privKey *rsa.PrivateKey) string {
	t.Helper()
	now := time.Now()
	claims := handlers.JWTClaims{
		UserID:   user.ID,
		Username: user.Username,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "dbBackend",
			Subject:   strconv.FormatInt(user.ID, 10),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signed, err := token.SignedString(privKey)
	if err != nil {
		t.Fatalf("failed to sign test JWT: %v", err)
	}
	return "Bearer " + signed
}

// doTestRequest executes an HTTP request with an optional auth header and JSON payload.
func doTestRequest(router http.Handler, method, path, authHeader string, payload any) *httptest.ResponseRecorder {
	return testutil.DoJSONRequest(router, method, path, payload, testutil.WithAuth(authHeader))
}
