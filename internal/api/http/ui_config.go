package apihttp

import (
	"net/http"
)

func (s *Server) uiConfig(w http.ResponseWriter, r *http.Request) {
	cfg := s.cfg
	writeJSON(w, http.StatusOK, map[string]any{
		"authMode":              cfg.AuthMode,
		"oidcIssuer":            cfg.OIDCIssuer,
		"oidcAudience":          cfg.OIDCAudience,
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
	writeJSON(w, http.StatusOK, map[string]any{
		"id":         sub.ID,
		"roles":      sub.Roles,
		"attributes": sub.Attributes,
	})
}
