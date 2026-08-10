package apihttp

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"github.com/l-ra/knowledge-core/internal/auth"
	"github.com/l-ra/knowledge-core/internal/bootstrap"
	"github.com/l-ra/knowledge-core/internal/config"
)

type Authenticator interface {
	Authenticate(r *http.Request) (auth.Subject, error)
}

type DevAuthenticator struct {
	BootstrapAdmin string
}

func NewAuthenticator(cfg config.Config) Authenticator {
	switch cfg.AuthMode {
	case "oidc":
		return &OIDCAuthenticator{
			Issuer:         cfg.OIDCIssuer,
			Audience:       cfg.OIDCAudience,
			BootstrapAdmin: cfg.BootstrapAdminSubject,
		}
	case "bootstrap":
		if cfg.BootstrapPasswordFile != "" {
			os.Setenv("KC_BOOTSTRAP_PASSWORD_FILE", cfg.BootstrapPasswordFile)
		}
		return NewBootstrapAuthenticator(cfg.BootstrapAdminSubject, func() (string, error) {
			return bootstrap.ReadPassword(bootstrap.PasswordFile())
		})
	default:
		return &DevAuthenticator{BootstrapAdmin: cfg.BootstrapAdminSubject}
	}
}

func (a *DevAuthenticator) Authenticate(r *http.Request) (auth.Subject, error) {
	subjectID := strings.TrimSpace(r.Header.Get("X-Subject"))
	if subjectID == "" {
		return auth.Subject{}, errUnauthenticated
	}
	roles := splitCSV(r.Header.Get("X-Roles"))
	if subjectID == a.BootstrapAdmin && !contains(roles, "admin") {
		roles = append(roles, "admin")
	}
	attrs := map[string]string{}
	if raw := r.Header.Get("X-Subject-Attributes"); raw != "" {
		_ = json.Unmarshal([]byte(raw), &attrs)
	}
	return auth.Subject{ID: subjectID, Roles: roles, Attributes: attrs}, nil
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func contains(ss []string, v string) bool {
	for _, s := range ss {
		if s == v {
			return true
		}
	}
	return false
}
