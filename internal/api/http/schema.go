package apihttp

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/l-ra/knowledge-core/internal/datatype"
	"github.com/l-ra/knowledge-core/internal/domain"
	"github.com/l-ra/knowledge-core/internal/validate"
)

func (s *Server) listClasses(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	items, next, err := s.engine.ListClasses(r.Context(), limit, r.URL.Query().Get("cursor"))
	if err != nil {
		writeEngineError(w, err)
		return
	}
	out := make([]any, 0, len(items))
	for i := range items {
		out = append(out, classDTO(&items[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "nextCursor": next})
}

type createClassReq struct {
	PackageCode  string            `json:"packageCode,omitempty"`
	Labels       map[string]string `json:"labels"`
	Descriptions map[string]string `json:"descriptions,omitempty"`
	SubClassOf   string            `json:"subClassOf,omitempty"`
	IRILocal     string            `json:"iriLocal,omitempty"`
}

func (s *Server) createClass(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	var req createClassReq
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	meta := writeMetaFromRequest(r, "createClass", hashBody(body))
	res, err := s.engine.CreateClass(r.Context(), meta, domain.CreateClassInput{
		PackageCode: req.PackageCode, Labels: req.Labels, Descriptions: req.Descriptions,
		SubClassOf: req.SubClassOf, IRILocal: req.IRILocal,
	})
	if err != nil {
		writeEngineError(w, err)
		return
	}
	status := http.StatusCreated
	if res.Replay {
		status = http.StatusOK
	}
	writeJSON(w, status, writeResponse(classDTO(&res.Value), res.ChangeSet))
}

func (s *Server) getClass(w http.ResponseWriter, r *http.Request) {
	c, err := s.engine.GetClass(r.Context(), pathParam(r, "cid"))
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, classDTO(c))
}

func (s *Server) listShapes(w http.ResponseWriter, r *http.Request) {
	items, err := s.engine.ListShapes(r.Context(), r.URL.Query().Get("package"))
	if err != nil {
		writeEngineError(w, err)
		return
	}
	out := make([]any, 0, len(items))
	for i := range items {
		out = append(out, shapeDTO(&items[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

type createShapeReq struct {
	Code        string               `json:"code"`
	ClassID     string               `json:"classId"`
	PackageCode string               `json:"packageCode,omitempty"`
	Document    domain.ShapeDocument `json:"document"`
}

func (s *Server) createShape(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	var req createShapeReq
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	sh, err := s.engine.CreateShape(r.Context(), domain.CreateShapeInput{
		Code: req.Code, ClassID: req.ClassID, PackageCode: req.PackageCode, Document: req.Document,
	})
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, shapeDTO(sh))
}

func (s *Server) getShape(w http.ResponseWriter, r *http.Request) {
	sh, err := s.engine.GetShape(r.Context(), pathParam(r, "code"))
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, shapeDTO(sh))
}

func (s *Server) getSchemaConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.engine.GetSchemaConfig(r.Context())
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, schemaConfigDTO(cfg))
}

type updateSchemaConfigReq struct {
	InstanceOfProperty string   `json:"instanceOfProperty"`
	ModelProperties    []string `json:"modelProperties"`
}

func (s *Server) putSchemaConfig(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	var req updateSchemaConfigReq
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	cfg, err := s.engine.UpdateSchemaConfig(r.Context(), domain.ModelSchemaConfig{
		InstanceOfProperty: req.InstanceOfProperty,
		ModelProperties:    req.ModelProperties,
	})
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, schemaConfigDTO(cfg))
}

func (s *Server) getEntityValidation(w http.ResponseWriter, r *http.Request) {
	qid := pathParam(r, "qid")
	res, err := s.engine.ValidateEntity(r.Context(), qid, validate.Options{})
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, validationDTO(res))
}

type createValidationReportReq struct {
	Scope     string   `json:"scope,omitempty"`
	EntityIDs []string `json:"entityIds"`
	Persist   bool     `json:"persist"`
}

func (s *Server) createValidationReport(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	var req createValidationReportReq
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	meta := writeMetaFromRequest(r, "createValidationReport", hashBody(body))
	rep, err := s.engine.CreateValidationReport(r.Context(), meta, domain.CreateValidationReportInput{
		Scope: req.Scope, EntityQIDs: req.EntityIDs, Persist: req.Persist,
	})
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, validationReportDTO(rep))
}

func (s *Server) getValidationReport(w http.ResponseWriter, r *http.Request) {
	rep, err := s.engine.GetValidationReport(r.Context(), pathParam(r, "id"))
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, validationReportDTO(rep))
}

func classDTO(c *domain.ClassDefinition) map[string]any {
	out := map[string]any{
		"id": c.PublicID, "canonicalId": c.ID, "status": c.Status,
		"displayId": datatype.PackageDisplayID(c.PackageCode, c.IRILocal, c.PublicID),
		"labels":    c.Labels, "descriptions": c.Descriptions,
		"document":  c.Document,
		"createdAt": c.CreatedAt, "updatedAt": c.UpdatedAt,
	}
	if c.PackageCode != "" {
		out["packageCode"] = c.PackageCode
	}
	out["iriLocal"] = c.IRILocal
	if c.IRI != "" {
		out["iri"] = c.IRI
	}
	if len(c.EffectiveClasses) > 0 {
		out["effectiveClasses"] = c.EffectiveClasses
	}
	return out
}

func shapeDTO(sh *domain.ShapeProfile) map[string]any {
	out := map[string]any{
		"id": sh.ID, "code": sh.Code, "classId": sh.ClassPID,
		"document":  sh.Document,
		"createdAt": sh.CreatedAt, "updatedAt": sh.UpdatedAt,
	}
	if sh.PackageCode != "" {
		out["packageCode"] = sh.PackageCode
	}
	return out
}

func schemaConfigDTO(cfg *domain.ModelSchemaConfig) map[string]any {
	return map[string]any{
		"instanceOfProperty": cfg.InstanceOfProperty,
		"modelProperties":    cfg.ModelProperties,
		"updatedAt":          cfg.UpdatedAt,
	}
}

func validationDTO(v *domain.ValidationResult) map[string]any {
	findings := make([]map[string]any, 0, len(v.Findings))
	for _, f := range v.Findings {
		m := map[string]any{"code": f.Code, "severity": f.Severity, "message": f.Message}
		if f.EntityID != "" {
			m["entityId"] = f.EntityID
		}
		if f.PropertyID != "" {
			m["propertyId"] = f.PropertyID
		}
		if f.StatementID != "" {
			m["statementId"] = f.StatementID
		}
		if f.ClassID != "" {
			m["classId"] = f.ClassID
		}
		if f.ShapeCode != "" {
			m["shapeCode"] = f.ShapeCode
		}
		findings = append(findings, m)
	}
	return map[string]any{
		"entityId": v.EntityID,
		"findings": findings,
		"summary":  v.Summary,
	}
}

func validationReportDTO(rep *domain.ValidationReport) map[string]any {
	out := map[string]any{
		"id": rep.ID, "scope": rep.Scope,
		"findings": rep.Findings, "summary": rep.Summary,
		"createdBy": rep.CreatedBy, "createdAt": rep.CreatedAt,
	}
	if rep.EntityQID != "" {
		out["entityId"] = rep.EntityQID
	}
	return out
}
