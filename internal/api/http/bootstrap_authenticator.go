package apihttp

import (
	"net/http"
	"strings"
	"sync"

	"github.com/l-ra/knowledge-core/internal/auth"
)

type BootstrapAuthenticator struct {
	BootstrapAdmin string
	password       func() (string, error)

	mu sync.RWMutex
	cached string
}

func NewBootstrapAuthenticator(admin string, passwordFn func() (string, error)) *BootstrapAuthenticator {
	return &BootstrapAuthenticator{
		BootstrapAdmin: admin,
		password:       passwordFn,
	}
}

func (a *BootstrapAuthenticator) Authenticate(r *http.Request) (auth.Subject, error) {
	token := extractBearerToken(r)
	if token == "" {
		token = strings.TrimSpace(r.Header.Get("X-Admin-Password"))
	}
	if token == "" {
		return auth.Subject{}, errUnauthenticated
	}

	expected, err := a.currentPassword()
	if err != nil {
		return auth.Subject{}, err
	}
	if token != expected {
		return auth.Subject{}, errUnauthenticated
	}

	subjectID := a.BootstrapAdmin
	if subjectID == "" {
		subjectID = "admin"
	}
	return auth.Subject{ID: subjectID, Roles: []string{"admin"}, Attributes: map[string]string{}}, nil
}

func (a *BootstrapAuthenticator) currentPassword() (string, error) {
	a.mu.RLock()
	if a.cached != "" {
		defer a.mu.RUnlock()
		return a.cached, nil
	}
	a.mu.RUnlock()

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cached != "" {
		return a.cached, nil
	}
	pw, err := a.password()
	if err != nil {
		return "", err
	}
	a.cached = pw
	return pw, nil
}

func extractBearerToken(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
}
