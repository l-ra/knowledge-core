package apihttp

import (
	"net/http"
	"strings"
)

func (s *Server) uiConfig(w http.ResponseWriter, r *http.Request) {
	cfg := s.liveConfig()
	writeJSON(w, http.StatusOK, map[string]any{
		"authMode":              cfg.AuthMode,
		"oidcIssuer":            cfg.OIDCIssuer,
		"oidcClientId":          cfg.EffectiveClientID(),
		"oidcAudience":          cfg.EffectiveAudience(),
		"bootstrapAdminSubject": cfg.BootstrapAdminSubject,
		"uiBasePath":            "/ui",
		"oidcRedirectPath":      "/ui/callback",
	})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	sub, ok := subjectFromRequest(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	display := sub.ID
	for _, key := range []string{"preferred_username", "name", "email"} {
		if v := strings.TrimSpace(sub.Attributes[key]); v != "" {
			display = v
			break
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":          sub.ID,
		"displayName": display,
		"roles":       sub.Roles,
		"attributes":  sub.Attributes,
	})
}
