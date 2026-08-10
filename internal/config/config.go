package config

import (
	"fmt"
	"os"
	"strings"
)

type Config struct {
	HTTPAddr               string
	DatabaseURL            string
	LogLevel               string
	AuthMode               string
	BootstrapAdminSubject  string
	BootstrapPasswordFile  string
	BootstrapAdminPassword string
	OIDCIssuer             string
	OIDCClientID           string
	OIDCAudience           string
}

func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:               getenv("KC_HTTP_ADDR", ":8080"),
		DatabaseURL:            getenv("KC_DATABASE_URL", "postgres://kc:kc@localhost:5433/knowledge_core?sslmode=disable"),
		LogLevel:               strings.ToLower(getenv("KC_LOG_LEVEL", "info")),
		AuthMode:               strings.ToLower(getenv("KC_AUTH_MODE", "dev")),
		BootstrapAdminSubject:  getenv("KC_BOOTSTRAP_ADMIN_SUBJECT", "admin"),
		BootstrapPasswordFile:  getenv("KC_BOOTSTRAP_PASSWORD_FILE", ""),
		BootstrapAdminPassword: os.Getenv("KC_BOOTSTRAP_ADMIN_PASSWORD"),
		OIDCIssuer:             os.Getenv("KC_OIDC_ISSUER"),
		OIDCClientID:           firstNonEmpty(os.Getenv("KC_OIDC_CLIENT_ID"), os.Getenv("KC_OIDC_AUDIENCE")),
		OIDCAudience:           os.Getenv("KC_OIDC_AUDIENCE"),
	}
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("KC_DATABASE_URL is required")
	}
	switch cfg.AuthMode {
	case "dev", "oidc", "bootstrap":
	default:
		return Config{}, fmt.Errorf("KC_AUTH_MODE must be dev, oidc, or bootstrap")
	}
	if cfg.AuthMode == "oidc" && cfg.OIDCIssuer == "" {
		return Config{}, fmt.Errorf("KC_OIDC_ISSUER is required when KC_AUTH_MODE=oidc")
	}
	return cfg, nil
}

// EffectiveClientID is the public OIDC client_id used by the SPA (PKCE).
func (c Config) EffectiveClientID() string {
	if c.OIDCClientID != "" {
		return c.OIDCClientID
	}
	return c.OIDCAudience
}

// EffectiveAudience is the JWT aud to verify; empty skips audience check.
func (c Config) EffectiveAudience() string {
	if c.OIDCAudience != "" {
		return c.OIDCAudience
	}
	return c.OIDCClientID
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
