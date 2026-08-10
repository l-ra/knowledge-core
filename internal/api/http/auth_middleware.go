package apihttp

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/rasekl/knowledge-core/internal/auth"
)

var errUnauthenticated = errors.New("unauthenticated")

type OIDCAuthenticator struct {
	Issuer         string
	Audience       string
	BootstrapAdmin string

	mu       sync.Mutex
	verifier *oidc.IDTokenVerifier
	initErr  error
}

func (a *OIDCAuthenticator) Authenticate(r *http.Request) (auth.Subject, error) {
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return auth.Subject{}, errUnauthenticated
	}
	token := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
	if token == "" {
		return auth.Subject{}, errUnauthenticated
	}

	verifier, err := a.verifierFor(r.Context())
	if err != nil {
		return auth.Subject{}, err
	}
	idToken, err := verifier.Verify(r.Context(), token)
	if err != nil {
		return auth.Subject{}, errUnauthenticated
	}

	var claims struct {
		Sub               string   `json:"sub"`
		Email             string   `json:"email"`
		PreferredUsername string   `json:"preferred_username"`
		Roles             []string `json:"roles"`
	}
	_ = idToken.Claims(&claims)

	subjectID := claims.Sub
	if subjectID == "" {
		subjectID = claims.PreferredUsername
	}
	if subjectID == "" {
		subjectID = claims.Email
	}
	if subjectID == "" {
		return auth.Subject{}, errUnauthenticated
	}

	roles := append([]string{}, claims.Roles...)
	if subjectID == a.BootstrapAdmin && !contains(roles, "admin") {
		roles = append(roles, "admin")
	}
	return auth.Subject{ID: subjectID, Roles: roles, Attributes: map[string]string{}}, nil
}

func (a *OIDCAuthenticator) verifierFor(ctx context.Context) (*oidc.IDTokenVerifier, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.verifier != nil {
		return a.verifier, nil
	}
	if a.initErr != nil {
		return nil, a.initErr
	}
	provider, err := oidc.NewProvider(ctx, a.Issuer)
	if err != nil {
		a.initErr = err
		return nil, err
	}
	cfg := &oidc.Config{SkipClientIDCheck: a.Audience == ""}
	if a.Audience != "" {
		cfg.ClientID = a.Audience
		cfg.SkipClientIDCheck = false
	}
	a.verifier = provider.Verifier(cfg)
	return a.verifier, nil
}

func authMiddleware(authn Authenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			subject, err := authn.Authenticate(r)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "unauthenticated")
				return
			}
			ctx := auth.WithSubject(r.Context(), subject)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
