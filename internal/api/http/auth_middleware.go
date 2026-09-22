package apihttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/l-ra/knowledge-core/internal/auth"
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

// stringListClaim accepts a JSON string array or a single space/CSV-separated string.
type stringListClaim []string

func (c *stringListClaim) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*c = nil
		return nil
	}
	var arr []string
	if err := json.Unmarshal(data, &arr); err == nil {
		*c = normalizeStringList(arr)
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		*c = nil
		return nil
	}
	*c = splitClaimList(s)
	return nil
}

func splitClaimList(s string) []string {
	fields := strings.FieldsFunc(strings.TrimSpace(s), func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	})
	return normalizeStringList(fields)
}

func normalizeStringList(in []string) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

// mergeRoleClaims unions roles and groups into Subject.Roles (roles first, then new from groups).
func mergeRoleClaims(roles, groups []string) []string {
	seen := make(map[string]struct{}, len(roles)+len(groups))
	out := make([]string, 0, len(roles)+len(groups))
	for _, list := range [][]string{roles, groups} {
		for _, v := range list {
			v = strings.TrimSpace(v)
			if v == "" {
				continue
			}
			if _, ok := seen[v]; ok {
				continue
			}
			seen[v] = struct{}{}
			out = append(out, v)
		}
	}
	return out
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
		Sub               string          `json:"sub"`
		Email             string          `json:"email"`
		Name              string          `json:"name"`
		PreferredUsername string          `json:"preferred_username"`
		Roles             stringListClaim `json:"roles"`
		Groups            stringListClaim `json:"groups"`
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

	roles := mergeRoleClaims([]string(claims.Roles), []string(claims.Groups))
	if subjectID == a.BootstrapAdmin && !contains(roles, "admin") {
		roles = append(roles, "admin")
	}
	attrs := map[string]string{}
	if claims.PreferredUsername != "" {
		attrs["preferred_username"] = claims.PreferredUsername
	}
	if claims.Name != "" {
		attrs["name"] = claims.Name
	}
	if claims.Email != "" {
		attrs["email"] = claims.Email
	}
	return auth.Subject{ID: subjectID, Roles: roles, Attributes: attrs}, nil
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
