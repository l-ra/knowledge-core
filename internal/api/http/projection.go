package apihttp

import (
	"net/http"
	"strconv"
)

func (s *Server) processOutbox(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	n, err := s.engine.ProcessOutbox(r.Context(), limit)
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"processed": n})
}

func (s *Server) rebuildSearchProjection(w http.ResponseWriter, r *http.Request) {
	if err := s.engine.RebuildSearchProjection(r.Context()); err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) searchProjection(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, http.StatusBadRequest, "q required")
		return
	}
	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	hits, err := s.engine.SearchProjection(r.Context(), q, limit)
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": hits})
}
