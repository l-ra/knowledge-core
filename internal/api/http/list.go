package apihttp

import (
	"net/http"
	"strconv"

	"github.com/l-ra/knowledge-core/internal/store"
)

func (s *Server) listEntities(w http.ResponseWriter, r *http.Request) {
	opt := store.ListOptions{
		Query:       r.URL.Query().Get("q"),
		Cursor:      r.URL.Query().Get("cursor"),
		Kind:        r.URL.Query().Get("kind"),
		PackageCode: r.URL.Query().Get("package"),
		Limit:       50,
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			opt.Limit = n
		}
	}
	items, next, err := s.engine.ListEntities(r.Context(), opt)
	if err != nil {
		writeEngineError(w, err)
		return
	}
	out := make([]any, 0, len(items))
	for i := range items {
		out = append(out, entityDTO(&items[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "nextCursor": next})
}

func (s *Server) listProperties(w http.ResponseWriter, r *http.Request) {
	opt := store.ListOptions{
		Query:  r.URL.Query().Get("q"),
		Cursor: r.URL.Query().Get("cursor"),
		Limit:  50,
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			opt.Limit = n
		}
	}
	items, next, err := s.engine.ListProperties(r.Context(), opt)
	if err != nil {
		writeEngineError(w, err)
		return
	}
	out := make([]any, 0, len(items))
	for i := range items {
		out = append(out, propertyDTO(&items[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "nextCursor": next})
}

func (s *Server) listLenses(w http.ResponseWriter, r *http.Request) {
	items, err := s.engine.ListLenses(r.Context())
	if err != nil {
		writeEngineError(w, err)
		return
	}
	out := make([]any, 0, len(items))
	for i := range items {
		out = append(out, lensDTO(&items[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (s *Server) listPackages(w http.ResponseWriter, r *http.Request) {
	items, err := s.engine.ListPackages(r.Context())
	if err != nil {
		writeEngineError(w, err)
		return
	}
	out := make([]any, 0, len(items))
	for i := range items {
		out = append(out, packageDTO(&items[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}
