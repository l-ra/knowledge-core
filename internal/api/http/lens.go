package apihttp

import (
	"encoding/json"
	"net/http"

	"github.com/l-ra/knowledge-core/internal/domain"
)

type createLensReq struct {
	Code     string              `json:"code"`
	Labels   map[string]string   `json:"labels"`
	Document domain.LensDocument `json:"document"`
}

func (s *Server) createLens(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	var req createLensReq
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	meta := writeMetaFromRequest(r, "createLens", hashBody(body))
	if rejectIfOpenChangeSet(w, meta, "createLens") {
		return
	}
	lens, err := s.engine.CreateLens(r.Context(), domain.CreateLensInput{
		Code: req.Code, Labels: req.Labels, Document: req.Document,
	})
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, lensDTO(lens))
}

func (s *Server) getLens(w http.ResponseWriter, r *http.Request) {
	lens, err := s.engine.GetLens(r.Context(), pathParam(r, "code"))
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, lensDTO(lens))
}

func (s *Server) getLensInstance(w http.ResponseWriter, r *http.Request) {
	data, err := s.engine.ReadLensInstance(r.Context(), pathParam(r, "code"), pathParam(r, "key"))
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, data)
}

func (s *Server) patchLensInstance(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	var req domain.LensPatchInput
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	meta := writeMetaFromRequest(r, "patchLens", hashBody(body))
	data, err := s.engine.PatchLensInstance(r.Context(), meta, pathParam(r, "code"), pathParam(r, "key"), req)
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data})
}

func lensDTO(l *domain.LensDefinition) map[string]any {
	return map[string]any{
		"code": l.Code, "version": l.Version, "labels": l.Labels, "document": l.Document,
		"createdAt": l.CreatedAt, "updatedAt": l.UpdatedAt,
	}
}
