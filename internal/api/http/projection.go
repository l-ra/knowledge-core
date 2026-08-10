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

func (s *Server) rebuildRDFProjection(w http.ResponseWriter, r *http.Request) {
	if err := s.engine.RebuildRDFProjection(r.Context()); err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) exportRDF(w http.ResponseWriter, r *http.Request) {
	nt, err := s.engine.ExportRDF(r.Context())
	if err != nil {
		writeEngineError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/n-triples")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(nt))
}
