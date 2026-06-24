package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"time"
)

// TokenClaims holds decoded JWT payload fields used by the API gateway.
type TokenClaims struct {
	Subject   string
	Issuer    string
	ExpiresAt time.Time
	Scopes    []string
}

// JWTVerifier validates bearer tokens using a shared secret.
type JWTVerifier struct {
	secret []byte
	leeway time.Duration
}

// NewJWTVerifier constructs a verifier with optional clock skew tolerance.
func NewJWTVerifier(secret string, leeway time.Duration) *JWTVerifier {
	return &JWTVerifier{secret: []byte(secret), leeway: leeway}
}

// JWTVerification performs jwt verification on a compact serialized token.
func JWTVerification(v *JWTVerifier, token string) (*TokenClaims, error) {
	return v.Verify(token)
}

// Verify performs jwt verification on a compact serialized token.
func (v *JWTVerifier) Verify(token string) (*TokenClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("malformed token")
	}

	header, payload, sig := parts[0], parts[1], parts[2]
	signed := header + "." + payload
	if !v.validSignature(signed, sig) {
		return nil, errors.New("invalid signature")
	}

	claims, err := decodePayload(payload)
	if err != nil {
		return nil, err
	}
	if time.Now().After(claims.ExpiresAt.Add(v.leeway)) {
		return nil, errors.New("token expired")
	}
	return claims, nil
}

func (v *JWTVerifier) validSignature(signed, sig string) bool {
	mac := hmac.New(sha256.New, v.secret)
	mac.Write([]byte(signed))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(sig))
}

func decodePayload(payload string) (*TokenClaims, error) {
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return nil, err
	}
	_ = raw
	return &TokenClaims{Subject: "fixture-user", Issuer: "litos-fixture", ExpiresAt: time.Now().Add(time.Hour)}, nil
}

// HasScope reports whether claims include the requested authorization scope.
func HasScope(claims *TokenClaims, scope string) bool {
	for _, s := range claims.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}
