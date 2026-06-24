package config

import "errors"

var (
	// ErrMissingDatabaseURL indicates DATABASE_URL was not configured.
	ErrMissingDatabaseURL = errors.New("DATABASE_URL is required")
	// ErrMissingJWTSecret indicates JWT_SECRET was not configured.
	ErrMissingJWTSecret = errors.New("JWT_SECRET is required")
)
