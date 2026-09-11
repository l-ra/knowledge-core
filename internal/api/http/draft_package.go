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

func (s *Server) openChangeSet(w http.ResponseWriter, r *http.Request) {
	body, _ := readBody(r)
	var req struct {
		Comment       string `json:"comment"`
		OperationType string `json:"operationType"`
	}
	if len(body) > 0 {
		_ = json.Unmarshal(body, &req)
	}
	meta := writeMetaFromRequest(r, "open", hashBody(body))
	cs, err := s.engine.OpenChangeSet(r.Context(), meta, domain.OpenChangeSetInput{
		Comment: req.Comment, OperationType: req.OperationType,
	})
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, writeResponse(changeSetDTO(cs), cs))
}

func (s *Server) commitOpenChangeSet(w http.ResponseWriter, r *http.Request) {
	body, _ := readBody(r)
	meta := writeMetaFromRequest(r, "openCommit", hashBody(body))
	res, err := s.engine.CommitOpenChangeSet(r.Context(), meta, pathParam(r, "cid"))
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
	writeJSON(w, http.StatusOK, writeResponse(changeSetDTO(&res.Value), res.ChangeSet))
}

func (s *Server) cancelOpenChangeSet(w http.ResponseWriter, r *http.Request) {
	body, _ := readBody(r)
	meta := writeMetaFromRequest(r, "openCancel", hashBody(body))
	cs, err := s.engine.CancelOpenChangeSet(r.Context(), meta, pathParam(r, "cid"))
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, writeResponse(changeSetDTO(cs), cs))
}

func (s *Server) updateOpenChangeSet(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	var req struct {
		Comment       *string `json:"comment"`
		OperationType *string `json:"operationType"`
	}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
	}
	if req.Comment == nil && req.OperationType == nil {
		writeError(w, http.StatusBadRequest, "comment or operationType required")
		return
	}
	meta := writeMetaFromRequest(r, "openUpdate", hashBody(body))
	cs, err := s.engine.UpdateOpenChangeSet(r.Context(), meta, pathParam(r, "cid"), domain.UpdateOpenChangeSetInput{
		Comment: req.Comment, OperationType: req.OperationType,
	})
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, writeResponse(changeSetDTO(cs), cs))
}
