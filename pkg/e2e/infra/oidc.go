package infra

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// OIDCServer is an in-process mock OIDC provider that generates an RSA key pair
// at startup and serves JWKS and token endpoints. It enables e2e tests to run
// with the production RHSSOAuthenticator instead of MIGRATION_PLANNER_AUTH=none.
type OIDCServer struct {
	server     *http.Server
	privateKey *rsa.PrivateKey
	kid        string
	baseURL    string
}

// tokenRequest is the JSON body for POST /token.
type tokenRequest struct {
	Username string `json:"username"`
	OrgID    string `json:"org_id"`
	Email    string `json:"email,omitempty"`
}

// tokenResponse is the JSON response for POST /token.
type tokenResponse struct {
	Token string `json:"token"`
}

// jwksResponse matches the standard JWKS format expected by MicahParks/jwkset.
type jwksResponse struct {
	Keys []jwkKey `json:"keys"`
}

type jwkKey struct {
	Kty string `json:"kty"`
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// oidcDiscovery is the response for /.well-known/openid-configuration.
type oidcDiscovery struct {
	Issuer  string `json:"issuer"`
	JWKSURI string `json:"jwks_uri"`
}

// NewOIDCServer creates a new mock OIDC server listening on the given address.
// It generates a 2048-bit RSA key pair and assigns a random kid.
func NewOIDCServer(addr string) (*OIDCServer, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("generating RSA key: %w", err)
	}

	// Resolve the actual port (useful when addr uses ":0")
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listening on %s: %w", addr, err)
	}

	actualAddr := listener.Addr().String()
	baseURL := fmt.Sprintf("http://%s", actualAddr)

	o := &OIDCServer{
		privateKey: privateKey,
		kid:        uuid.NewString(),
		baseURL:    baseURL,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", o.handleDiscovery)
	mux.HandleFunc("/openid-connect/certs", o.handleJWKS)
	mux.HandleFunc("/token", o.handleToken)

	o.server = &http.Server{
		Handler: mux,
	}

	// Start serving in a goroutine using the already-bound listener
	go func() {
		zap.S().Infof("OIDC mock server started on %s", actualAddr)
		if err := o.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			zap.S().Errorf("OIDC mock server error: %v", err)
		}
	}()

	return o, nil
}

// Stop gracefully shuts down the OIDC server.
func (o *OIDCServer) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return o.server.Shutdown(ctx)
}

// JWKSURL returns the URL to the JWKS endpoint.
func (o *OIDCServer) JWKSURL() string {
	return o.baseURL + "/openid-connect/certs"
}

// BaseURL returns the base URL of the OIDC server.
func (o *OIDCServer) BaseURL() string {
	return o.baseURL
}

// GenerateToken creates a signed RS256 JWT with the given user claims.
func (o *OIDCServer) GenerateToken(username, orgID, email string) (string, error) {
	type tokenClaims struct {
		Username string `json:"username"`
		OrgID    string `json:"org_id"`
		Email    string `json:"email,omitempty"`
		jwt.RegisteredClaims
	}

	claims := tokenClaims{
		Username: username,
		OrgID:    orgID,
		Email:    email,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Issuer:    o.baseURL,
			Subject:   username,
			ID:        uuid.NewString(),
			Audience:  jwt.ClaimStrings{"migration-planner"},
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = o.kid

	signed, err := token.SignedString(o.privateKey)
	if err != nil {
		return "", fmt.Errorf("signing token: %w", err)
	}

	return signed, nil
}

// handleDiscovery serves the OIDC discovery document.
func (o *OIDCServer) handleDiscovery(w http.ResponseWriter, _ *http.Request) {
	doc := oidcDiscovery{
		Issuer:  o.baseURL,
		JWKSURI: o.JWKSURL(),
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(doc)
}

// handleJWKS serves the JSON Web Key Set containing the RSA public key.
func (o *OIDCServer) handleJWKS(w http.ResponseWriter, _ *http.Request) {
	pub := &o.privateKey.PublicKey

	resp := jwksResponse{
		Keys: []jwkKey{
			{
				Kty: "RSA",
				Alg: "RS256",
				Kid: o.kid,
				Use: "sig",
				N:   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
				E:   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// handleToken generates a signed JWT for the given user claims.
func (o *OIDCServer) handleToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req tokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Username == "" {
		http.Error(w, "username is required", http.StatusBadRequest)
		return
	}

	signed, err := o.GenerateToken(req.Username, req.OrgID, req.Email)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to generate token: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(tokenResponse{Token: signed})
}
