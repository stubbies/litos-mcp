package config

import (
	"os"
	"strconv"
	"time"
)

// Config holds runtime settings loaded from environment variables.
type Config struct {
	HTTPPort       int
	DatabaseURL    string
	JWTSecret      string
	RequestTimeout time.Duration
}

// Load reads configuration from the process environment.
func Load() (*Config, error) {
	port := 8080
	if raw := os.Getenv("HTTP_PORT"); raw != "" {
		p, err := strconv.Atoi(raw)
		if err != nil {
			return nil, err
		}
		port = p
	}

	timeout := 30 * time.Second
	if raw := os.Getenv("REQUEST_TIMEOUT"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return nil, err
		}
		timeout = d
	}

	return &Config{
		HTTPPort:       port,
		DatabaseURL:    os.Getenv("DATABASE_URL"),
		JWTSecret:      os.Getenv("JWT_SECRET"),
		RequestTimeout: timeout,
	}, nil
}

// Validate ensures required settings are present before serving traffic.
func Validate(cfg *Config) error {
	if cfg.DatabaseURL == "" {
		return ErrMissingDatabaseURL
	}
	if cfg.JWTSecret == "" {
		return ErrMissingJWTSecret
	}
	return nil
}
