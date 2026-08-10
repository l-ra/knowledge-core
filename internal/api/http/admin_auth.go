package apihttp

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/l-ra/knowledge-core/internal/auth"
	"github.com/l-ra/knowledge-core/internal/config"
	"github.com/l-ra/knowledge-core/internal/store"
)

type authRuntimeDTO struct {
	AuthMode     string    `json:"authMode"`
	OIDCIssuer   string    `json:"oidcIssuer"`
	OIDCClientID string    `json:"oidcClientId"`
	OIDCAudience string    `json:"oidcAudience"`
	Source       string    `json:"source"` // env | runtime
	UpdatedAt    time.Time `json:"updatedAt,omitempty"`
	UpdatedBy    string    `json:"updatedBy,omitempty"`
	Instructions struct {
		IdP []string `json:"idp"`
		KC  []string `json:"knowledgeCore"`
	} `json:"instructions"`
}

func oidcSetupInstructions() (idp, kc []string) {
	idp = []string{
		"Open your IdP (e.g. Pocket ID) and complete /setup if needed.",
		"Create a public OIDC client with PKCE enabled.",
		"Set the redirect URI to {origin}/ui/callback (do not change client_id after create — it is immutable in Pocket ID).",
		"Copy the generated client_id from the IdP.",
	}
	kc = []string{
		"Paste the IdP issuer URL (Pocket ID APP_URL) below.",
		"Paste the client_id from the IdP (Knowledge Core does not invent it).",
		"Optionally set audience if it differs from client_id; leave empty to use client_id.",
		"Save to enable OIDC. After that, sign in with OIDC (bootstrap/dev login stops working for API auth).",
	}
	return idp, kc
}

func (s *Server) getAdminAuth(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	cfg := s.liveConfig()
	dto := authRuntimeDTO{
		AuthMode:     cfg.AuthMode,
		OIDCIssuer:   cfg.OIDCIssuer,
		OIDCClientID: cfg.EffectiveClientID(),
		OIDCAudience: cfg.OIDCAudience,
		Source:       "env",
	}
	idp, kc := oidcSetupInstructions()
	dto.Instructions.IdP = idp
	dto.Instructions.KC = kc

	if row, err := s.store.GetAuthRuntime(r.Context()); err == nil && row != nil {
		dto.Source = "runtime"
		dto.UpdatedAt = row.UpdatedAt
		dto.UpdatedBy = row.UpdatedBy
		dto.AuthMode = row.AuthMode
		dto.OIDCIssuer = row.OIDCIssuer
		dto.OIDCClientID = row.OIDCClientID
		dto.OIDCAudience = row.OIDCAudience
	}
	writeJSON(w, http.StatusOK, dto)
}

func (s *Server) putAdminAuth(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var body struct {
		AuthMode     string `json:"authMode"`
		OIDCIssuer   string `json:"oidcIssuer"`
		OIDCClientID string `json:"oidcClientId"`
		OIDCAudience string `json:"oidcAudience"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	mode := strings.ToLower(strings.TrimSpace(body.AuthMode))
	if mode == "" {
		mode = "oidc"
	}
	switch mode {
	case "oidc", "bootstrap", "dev":
	default:
		writeError(w, http.StatusBadRequest, "authMode must be oidc, bootstrap, or dev")
		return
	}

	issuer := strings.TrimSpace(body.OIDCIssuer)
	clientID := strings.TrimSpace(body.OIDCClientID)
	audience := strings.TrimSpace(body.OIDCAudience)

	if mode == "oidc" {
		if issuer == "" {
			writeError(w, http.StatusBadRequest, "oidcIssuer is required for oidc mode")
			return
		}
		if clientID == "" {
			writeError(w, http.StatusBadRequest, "oidcClientId is required for oidc mode (create the client in the IdP first)")
			return
		}
		if err := validateOIDCIssuer(r.Context(), issuer); err != nil {
			writeError(w, http.StatusBadRequest, "oidcIssuer discovery failed: "+err.Error())
			return
		}
	}

	actor := "admin"
	if sub, ok := auth.SubjectFromContext(r.Context()); ok {
		actor = sub.ID
	}
	row := store.AuthRuntime{
		AuthMode:     mode,
		OIDCIssuer:   issuer,
		OIDCClientID: clientID,
		OIDCAudience: audience,
		UpdatedBy:    actor,
	}
	if err := s.store.UpsertAuthRuntime(r.Context(), row); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	cfg := s.liveConfig()
	cfg.AuthMode = mode
	cfg.OIDCIssuer = issuer
	cfg.OIDCClientID = clientID
	cfg.OIDCAudience = audience
	s.setLiveConfig(cfg)
	s.authn.Swap(NewAuthenticator(cfg))

	s.getAdminAuth(w, r)
}

func validateOIDCIssuer(ctx context.Context, issuer string) error {
	_, err := oidc.NewProvider(ctx, issuer)
	return err
}

func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	sub, ok := auth.SubjectFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return false
	}
	cfg := s.liveConfig()
	if auth.HasRole(sub, "admin") || sub.ID == cfg.BootstrapAdminSubject {
		return true
	}
	writeError(w, http.StatusForbidden, "admin role required")
	return false
}

func ApplyAuthRuntime(ctx context.Context, st *store.Store, cfg *config.Config) error {
	return applyAuthRuntime(ctx, st, cfg)
}

func applyAuthRuntime(ctx context.Context, st *store.Store, cfg *config.Config) error {
	row, err := st.GetAuthRuntime(ctx)
	if err != nil {
		return err
	}
	if row == nil {
		return nil
	}
	cfg.AuthMode = row.AuthMode
	cfg.OIDCIssuer = row.OIDCIssuer
	cfg.OIDCClientID = row.OIDCClientID
	cfg.OIDCAudience = row.OIDCAudience
	return nil
}
