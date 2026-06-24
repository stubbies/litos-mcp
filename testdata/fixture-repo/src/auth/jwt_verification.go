package auth

// ValidateAccessToken runs jwt verification checks for API gateway requests.
func ValidateAccessToken(v *JWTVerifier, token string) (*TokenClaims, error) {
	return JWTVerification(v, token)
}
