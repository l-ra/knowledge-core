package apihttp

import (
	"encoding/json"
	"net/http"

	"github.com/l-ra/knowledge-core/internal/domain"
)

func (s *Server) listPackageReleases(w http.ResponseWriter, r *http.Request) {
	items, err := s.engine.ListReleases(r.Context(), pathParam(r, "code"))
	if err != nil {
		writeEngineError(w, err)
		return
	}
	out := make([]any, 0, len(items))
	for i := range items {
		out = append(out, releaseDTO(&items[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (s *Server) listPackageObjects(w http.ResponseWriter, r *http.Request) {
	items, err := s.engine.ListPackageObjects(r.Context(), pathParam(r, "code"))
	if err != nil {
		writeEngineError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for _, o := range items {
		out = append(out, map[string]any{
			"objectType": o.ObjectType,
			"publicId":   o.PublicID,
			"displayId":  o.DisplayID,
			"revisionNo": o.RevisionNo,
			"labels":     o.Labels,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (s *Server) listObjectReleases(w http.ResponseWriter, r *http.Request) {
	items, err := s.engine.ListObjectReleases(r.Context(), pathParam(r, "id"))
	if err != nil {
		writeEngineError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for _, o := range items {
		out = append(out, map[string]any{
			"packageCode": o.PackageCode,
			"version":     o.Version,
			"revisionNo":  o.RevisionNo,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (s *Server) getChangeSetDraft(w http.ResponseWriter, r *http.Request) {
	d, err := s.engine.GetChangeSetDraft(r.Context())
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) putChangeSetDraft(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	var draft domain.ChangeSetDraft
	if err := json.Unmarshal(body, &draft); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	d, err := s.engine.PutChangeSetDraft(r.Context(), draft)
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) deleteChangeSetDraft(w http.ResponseWriter, r *http.Request) {
	if err := s.engine.DeleteChangeSetDraft(r.Context()); err != nil {
		writeEngineError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) commitChangeSetDraft(w http.ResponseWriter, r *http.Request) {
	body, _ := readBody(r)
	meta := writeMetaFromRequest(r, "draftCommit", hashBody(body))
	res, err := s.engine.CommitChangeSetDraft(r.Context(), meta)
	if err != nil {
		writeEngineError(w, err)
		return
	}
	if len(res.ResponseRaw) > 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(res.ResponseRaw)
		return
	}
	writeJSON(w, http.StatusOK, changeSetDTO(&res.Value))
}
