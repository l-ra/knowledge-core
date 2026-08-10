package apihttp

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/l-ra/knowledge-core/internal/auth"
	"github.com/l-ra/knowledge-core/internal/engine"
)

func (s *Server) listPolicies(w http.ResponseWriter, r *http.Request) {
	policies, err := s.engine.ListPolicies(r.Context())
	if err != nil {
		writeEngineError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(policies))
	for i := range policies {
		out = append(out, policyDTO(&policies[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"policies": out})
}

func (s *Server) getPolicy(w http.ResponseWriter, r *http.Request) {
	p, err := s.engine.GetPolicy(r.Context(), chi.URLParam(r, "name"))
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, policyDTO(p))
}

type upsertPolicyReq struct {
	Name     string              `json:"name"`
	Priority int                 `json:"priority"`
	Document auth.PolicyDocument `json:"document"`
}

func (s *Server) upsertPolicy(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	var req upsertPolicyReq
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	name := req.Name
	if name == "" {
		name = chi.URLParam(r, "name")
	}
	p, err := s.engine.UpsertPolicy(r.Context(), engine.UpsertPolicyInput{
		Name: name, Priority: req.Priority, Document: req.Document,
	})
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, policyDTO(p))
}

func (s *Server) deletePolicy(w http.ResponseWriter, r *http.Request) {
	if err := s.engine.DeletePolicy(r.Context(), chi.URLParam(r, "name")); err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func policyDTO(p *auth.Policy) map[string]any {
	return map[string]any{
		"name":     p.Name,
		"priority": p.Priority,
		"document": p.Document,
	}
}
