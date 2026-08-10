package config

import (
	"fmt"
	"os"
	"strings"
)

type Config struct {
	HTTPAddr              string
	DatabaseURL           string
	LogLevel              string
	AuthMode              string
	BootstrapAdminSubject string
	OIDCIssuer            string
	OIDCAudience          string
}

func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:              getenv("KC_HTTP_ADDR", ":8080"),
		DatabaseURL:             getenv("KC_DATABASE_URL", "postgres://kc:kc@localhost:5433/knowledge_core?sslmode=disable"),
		LogLevel:                strings.ToLower(getenv("KC_LOG_LEVEL", "info")),
		AuthMode:                strings.ToLower(getenv("KC_AUTH_MODE", "dev")),
		BootstrapAdminSubject:   getenv("KC_BOOTSTRAP_ADMIN_SUBJECT", "admin"),
		OIDCIssuer:            os.Getenv("KC_OIDC_ISSUER"),
		OIDCAudience:          os.Getenv("KC_OIDC_AUDIENCE"),
	}
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("KC_DATABASE_URL is required")
	}
	if cfg.AuthMode != "dev" && cfg.AuthMode != "oidc" {
		return Config{}, fmt.Errorf("KC_AUTH_MODE must be dev or oidc")
	}
	if cfg.AuthMode == "oidc" && cfg.OIDCIssuer == "" {
		return Config{}, fmt.Errorf("KC_OIDC_ISSUER is required when KC_AUTH_MODE=oidc")
	}
	return cfg, nil
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
