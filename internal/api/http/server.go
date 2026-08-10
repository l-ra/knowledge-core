package apihttp

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/l-ra/knowledge-core/internal/auth"
	"github.com/l-ra/knowledge-core/internal/config"
	"github.com/l-ra/knowledge-core/internal/datatype"
	"github.com/l-ra/knowledge-core/internal/domain"
	"github.com/l-ra/knowledge-core/internal/engine"
	"github.com/l-ra/knowledge-core/internal/metrics"
	"github.com/l-ra/knowledge-core/internal/store"
	"github.com/l-ra/knowledge-core/web/ui"
)

type Server struct {
	engine *engine.Engine
	store  *store.Store
	cfg    config.Config
}

func New(eng *engine.Engine, st *store.Store, authn Authenticator, cfg config.Config) http.Handler {
	s := &Server{engine: eng, store: st, cfg: cfg}
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))
	r.Use(metrics.Middleware)
	r.Use(correlationMiddleware)

	r.Get("/healthz", s.healthz)
	r.Handle("/metrics", metrics.Handler())
	r.Get("/v1/ui/config", s.uiConfig)

	r.Route("/v1", func(r chi.Router) {
		r.Use(authMiddleware(authn))
		r.Get("/me", s.me)

		r.Get("/entities", s.listEntities)
		r.Post("/entities", s.createEntity)
		r.Get("/entities/{qid}", s.getEntity)
		r.Patch("/entities/{qid}", s.updateEntity)
		r.Get("/entities/{qid}/history", s.getEntityHistory)
		r.Get("/entities/{qid}/statements", s.listEntityStatements)

		r.Get("/properties", s.listProperties)
		r.Post("/properties", s.createProperty)
		r.Get("/properties/{pid}", s.getProperty)

		r.Get("/packages", s.listPackages)
		r.Post("/packages", s.createPackage)
		r.Get("/packages/{code}", s.getPackage)
		r.Post("/packages/{code}/releases", s.publishRelease)
		r.Get("/packages/{code}/releases/{version}", s.getRelease)
		r.Get("/packages/{code}/releases/{version}/bundle", s.exportReleaseBundle)
		r.Post("/packages/{code}/releases/{version}/mutate", s.mutateRelease)
		r.Post("/releases/import", s.importRelease)

		r.Post("/references", s.createReference)
		r.Get("/references/{rid}", s.getReference)

		r.Post("/statements", s.createStatement)
		r.Get("/statements/{sid}", s.getStatement)
		r.Post("/statements/{sid}/revise", s.reviseStatement)
		r.Get("/statements/{sid}/history", s.getStatementHistory)

		r.Post("/changesets", s.applyChangeSet)
		r.Get("/changesets/{cid}", s.getChangeSet)

		r.Get("/policies", s.listPolicies)
		r.Post("/policies", s.upsertPolicy)
		r.Get("/policies/{name}", s.getPolicy)
		r.Put("/policies/{name}", s.upsertPolicy)
		r.Delete("/policies/{name}", s.deletePolicy)

		r.Get("/lenses", s.listLenses)
		r.Post("/lenses", s.createLens)
		r.Get("/lenses/{code}", s.getLens)
		r.Get("/lenses/{code}/instances/{key}", s.getLensInstance)
		r.Post("/lenses/{code}/instances/{key}/patch", s.patchLensInstance)
		r.Post("/graphql", s.graphql)

		r.Post("/projections/outbox/process", s.processOutbox)
		r.Post("/projections/search/rebuild", s.rebuildSearchProjection)
		r.Get("/projections/search", s.searchProjection)
		r.Post("/projections/rdf/rebuild", s.rebuildRDFProjection)
		r.Get("/projections/rdf", s.exportRDF)
	})

	mountUI(r)

	return r
}

func mountUI(r chi.Router) {
	sub, err := fs.Sub(ui.Assets, "dist")
	if err != nil {
		// UI not built yet — serve placeholder
		r.Get("/ui", func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "UI not built; run: npm run build in web/ui", http.StatusServiceUnavailable)
		})
		r.Get("/ui/*", func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "UI not built; run: npm run build in web/ui", http.StatusServiceUnavailable)
		})
		return
	}
	fileServer := http.FileServer(http.FS(sub))
	r.Get("/ui", func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, "/ui/", http.StatusFound)
	})
	r.Handle("/ui/*", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		path := strings.TrimPrefix(req.URL.Path, "/ui")
		if path == "" || path == "/" {
			path = "/index.html"
		}
		// SPA fallback: if asset missing, serve index.html
		f, err := sub.Open(strings.TrimPrefix(path, "/"))
		if err != nil {
			req.URL.Path = "/"
			http.StripPrefix("/ui", fileServer).ServeHTTP(w, req)
			return
		}
		_ = f.Close()
		http.StripPrefix("/ui", fileServer).ServeHTTP(w, req)
	}))
}

func subjectFromRequest(r *http.Request) (auth.Subject, bool) {
	return auth.SubjectFromContext(r.Context())
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Ping(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type createEntityReq struct {
	PackageCode  string            `json:"packageCode,omitempty"`
	Labels       map[string]string `json:"labels"`
	Descriptions map[string]string `json:"descriptions"`
}

func (s *Server) createEntity(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	var req createEntityReq
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	meta := writeMetaFromRequest(r, "createEntity", hashBody(body))
	res, err := s.engine.CreateEntity(r.Context(), meta, domain.CreateEntityInput{
		PackageCode: req.PackageCode, Labels: req.Labels, Descriptions: req.Descriptions,
	})
	if err != nil {
		writeEngineError(w, err)
		return
	}
	status := http.StatusCreated
	if res.Replay {
		status = http.StatusOK
	}
	writeJSON(w, status, writeResponse(entityDTO(&res.Value), res.ChangeSet))
}

func (s *Server) getEntity(w http.ResponseWriter, r *http.Request) {
	ent, err := s.engine.GetEntity(r.Context(), chi.URLParam(r, "qid"))
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, entityDTO(ent))
}

type updateEntityReq struct {
	Labels           map[string]string `json:"labels"`
	Descriptions     map[string]string `json:"descriptions"`
	ExpectedRevision int               `json:"expectedRevision"`
}

func (s *Server) updateEntity(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	var req updateEntityReq
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	meta := writeMetaFromRequest(r, "updateEntity", hashBody(body))
	res, err := s.engine.UpdateEntity(r.Context(), meta, chi.URLParam(r, "qid"), domain.UpdateEntityInput{
		Labels: req.Labels, Descriptions: req.Descriptions, ExpectedRevision: req.ExpectedRevision,
	})
	if err != nil {
		writeEngineError(w, err)
		return
	}
	status := http.StatusOK
	if !res.Replay && res.ChangeSet != nil {
		status = http.StatusOK
	}
	writeJSON(w, status, writeResponse(entityDTO(&res.Value), res.ChangeSet))
}

func (s *Server) getEntityHistory(w http.ResponseWriter, r *http.Request) {
	h, err := s.engine.GetEntityHistory(r.Context(), chi.URLParam(r, "qid"))
	if err != nil {
		writeEngineError(w, err)
		return
	}
	out := make([]any, 0, len(h))
	for i := range h {
		out = append(out, entityRevisionDTO(&h[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"revisions": out})
}

type createPropertyReq struct {
	PackageCode  string            `json:"packageCode,omitempty"`
	Datatype     string            `json:"datatype"`
	Labels       map[string]string `json:"labels"`
	Descriptions map[string]string `json:"descriptions"`
}

func (s *Server) createProperty(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	var req createPropertyReq
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	dt, err := datatype.ParseType(req.Datatype)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	meta := writeMetaFromRequest(r, "createProperty", hashBody(body))
	res, err := s.engine.CreateProperty(r.Context(), meta, domain.CreatePropertyInput{
		PackageCode: req.PackageCode, Datatype: dt, Labels: req.Labels, Descriptions: req.Descriptions,
	})
	if err != nil {
		writeEngineError(w, err)
		return
	}
	status := http.StatusCreated
	if res.Replay {
		status = http.StatusOK
	}
	writeJSON(w, status, writeResponse(propertyDTO(&res.Value), res.ChangeSet))
}

func (s *Server) getProperty(w http.ResponseWriter, r *http.Request) {
	p, err := s.engine.GetProperty(r.Context(), chi.URLParam(r, "pid"))
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, propertyDTO(p))
}

type createStatementReq struct {
	PackageCode  string                  `json:"packageCode,omitempty"`
	Subject      string                  `json:"subject"`
	Property     string                  `json:"property"`
	Value        datatype.Value          `json:"value"`
	Qualifiers   []domain.QualifierInput `json:"qualifiers,omitempty"`
	ReferenceIDs []string                `json:"referenceIds,omitempty"`
	ValidFrom    *time.Time              `json:"validFrom,omitempty"`
	ValidTo      *time.Time              `json:"validTo,omitempty"`
}

func (s *Server) createStatement(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	var req createStatementReq
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	meta := writeMetaFromRequest(r, "createStatement", hashBody(body))
	res, err := s.engine.CreateStatement(r.Context(), meta, domain.CreateStatementInput{
		PackageCode: req.PackageCode, SubjectPublicID: req.Subject, PropertyPublicID: req.Property, Value: req.Value,
		Qualifiers: req.Qualifiers, ReferenceIDs: req.ReferenceIDs,
		ValidFrom: req.ValidFrom, ValidTo: req.ValidTo,
	})
	if err != nil {
		writeEngineError(w, err)
		return
	}
	status := http.StatusCreated
	if res.Replay {
		status = http.StatusOK
	}
	writeJSON(w, status, writeResponse(statementDTO(&res.Value), res.ChangeSet))
}

type reviseStatementReq struct {
	ExpectedRevision int                     `json:"expectedRevision"`
	Value            *datatype.Value         `json:"value,omitempty"`
	Qualifiers       *[]domain.QualifierInput `json:"qualifiers,omitempty"`
	ReferenceIDs     *[]string               `json:"referenceIds,omitempty"`
	ValidFrom        *time.Time              `json:"validFrom,omitempty"`
	ValidTo          *time.Time              `json:"validTo,omitempty"`
}

type createReferenceReq struct {
	Fields map[string]any `json:"fields"`
}

func (s *Server) createReference(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	var req createReferenceReq
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	meta := writeMetaFromRequest(r, "createReference", hashBody(body))
	res, err := s.engine.CreateReference(r.Context(), meta, domain.CreateReferenceInput{Fields: req.Fields})
	if err != nil {
		writeEngineError(w, err)
		return
	}
	status := http.StatusCreated
	if res.Replay {
		status = http.StatusOK
	}
	writeJSON(w, status, writeResponse(referenceDTO(&res.Value), res.ChangeSet))
}

func (s *Server) getReference(w http.ResponseWriter, r *http.Request) {
	ref, err := s.engine.GetReference(r.Context(), chi.URLParam(r, "rid"))
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, referenceDTO(ref))
}

func (s *Server) reviseStatement(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	var req reviseStatementReq
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	meta := writeMetaFromRequest(r, "reviseStatement", hashBody(body))
	in := domain.ReviseStatementInput{ExpectedRevision: req.ExpectedRevision}
	if req.Value != nil {
		in.Value = req.Value
	}
	if req.Qualifiers != nil {
		in.ReplaceQualifiers = true
		in.Qualifiers = *req.Qualifiers
	}
	if req.ReferenceIDs != nil {
		in.ReplaceReferences = true
		in.ReferenceIDs = *req.ReferenceIDs
	}
	if req.ValidFrom != nil || req.ValidTo != nil {
		in.ReplaceValidTime = true
		in.ValidFrom = req.ValidFrom
		in.ValidTo = req.ValidTo
	}
	res, err := s.engine.ReviseStatement(r.Context(), meta, chi.URLParam(r, "sid"), in)
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, writeResponse(statementDTO(&res.Value), res.ChangeSet))
}

func (s *Server) getStatement(w http.ResponseWriter, r *http.Request) {
	st, err := s.engine.GetStatement(r.Context(), chi.URLParam(r, "sid"))
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, statementDTO(st))
}

func (s *Server) getStatementHistory(w http.ResponseWriter, r *http.Request) {
	h, err := s.engine.GetStatementHistory(r.Context(), chi.URLParam(r, "sid"))
	if err != nil {
		writeEngineError(w, err)
		return
	}
	out := make([]any, 0, len(h))
	for i := range h {
		out = append(out, statementRevisionDTO(&h[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"revisions": out})
}

func (s *Server) listEntityStatements(w http.ResponseWriter, r *http.Request) {
	list, err := s.engine.ListEntityStatements(r.Context(), chi.URLParam(r, "qid"))
	if err != nil {
		writeEngineError(w, err)
		return
	}
	out := make([]any, 0, len(list))
	for i := range list {
		out = append(out, statementDTO(&list[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"statements": out})
}

type applyChangeSetReq struct {
	OperationType string                   `json:"operationType"`
	Comment       string                   `json:"comment"`
	Operations    []domain.ChangeOperation `json:"operations"`
}

func (s *Server) applyChangeSet(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	var req applyChangeSetReq
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	meta := writeMetaFromRequest(r, req.OperationType, hashBody(body))
	if meta.OperationType == "" {
		meta.OperationType = "batch"
	}
	res, err := s.engine.ApplyChangeSet(r.Context(), meta, domain.ApplyChangeSetInput{
		OperationType: req.OperationType,
		Comment:       req.Comment,
		Operations:    req.Operations,
	})
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

func (s *Server) getChangeSet(w http.ResponseWriter, r *http.Request) {
	cs, err := s.engine.GetChangeSet(r.Context(), chi.URLParam(r, "cid"))
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, changeSetDTO(cs))
}

type createPackageReq struct {
	Code         string                    `json:"code"`
	Lifecycle    string                    `json:"lifecycle"`
	Labels       map[string]string         `json:"labels"`
	Dependencies []domain.PackageDependency `json:"dependencies,omitempty"`
}

func (s *Server) createPackage(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	var req createPackageReq
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	lc := domain.PackageLifecycle(req.Lifecycle)
	if lc == "" {
		lc = domain.PackageReleased
	}
	meta := writeMetaFromRequest(r, "createPackage", hashBody(body))
	res, err := s.engine.CreatePackage(r.Context(), meta, domain.CreatePackageInput{
		Code: req.Code, Lifecycle: lc, Labels: req.Labels, Dependencies: req.Dependencies,
	})
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, writeResponse(packageDTO(&res.Value), res.ChangeSet))
}

func (s *Server) getPackage(w http.ResponseWriter, r *http.Request) {
	pkg, err := s.engine.GetPackage(r.Context(), chi.URLParam(r, "code"))
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, packageDTO(pkg))
}

type publishReleaseReq struct {
	Version string `json:"version"`
}

func (s *Server) publishRelease(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	var req publishReleaseReq
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	meta := writeMetaFromRequest(r, "publishRelease", hashBody(body))
	res, err := s.engine.PublishRelease(r.Context(), meta, chi.URLParam(r, "code"), domain.PublishReleaseInput{
		Version: req.Version,
	})
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, writeResponse(releaseDTO(&res.Value), res.ChangeSet))
}

func (s *Server) getRelease(w http.ResponseWriter, r *http.Request) {
	rel, err := s.engine.GetRelease(r.Context(), chi.URLParam(r, "code"), chi.URLParam(r, "version"))
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, releaseDTO(rel))
}

func (s *Server) exportReleaseBundle(w http.ResponseWriter, r *http.Request) {
	bundle, err := s.engine.ExportReleaseBundle(r.Context(), chi.URLParam(r, "code"), chi.URLParam(r, "version"))
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, bundle)
}

func (s *Server) mutateRelease(w http.ResponseWriter, r *http.Request) {
	err := s.engine.MutateRelease(r.Context(), chi.URLParam(r, "code"), chi.URLParam(r, "version"))
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) importRelease(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	var bundle domain.Bundle
	if err := json.Unmarshal(body, &bundle); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	meta := writeMetaFromRequest(r, "importRelease", hashBody(body))
	res, err := s.engine.ImportReleaseBundle(r.Context(), meta, bundle)
	if err != nil {
		writeEngineError(w, err)
		return
	}
	status := http.StatusCreated
	if res.Replay {
		status = http.StatusOK
	}
	writeJSON(w, status, writeResponse(releaseDTO(&res.Value), res.ChangeSet))
}

func packageDTO(p *domain.Package) map[string]any {
	deps := make([]map[string]string, 0, len(p.Dependencies))
	for _, d := range p.Dependencies {
		deps = append(deps, map[string]string{"dependsOnCode": d.DependsOnCode, "versionRange": d.VersionRange})
	}
	return map[string]any{
		"code": p.Code, "lifecycle": p.Lifecycle, "labels": p.Labels,
		"dependencies": deps,
		"createdAt": p.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updatedAt": p.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func releaseDTO(rel *domain.Release) map[string]any {
	objs := make([]map[string]any, 0, len(rel.Objects))
	for _, o := range rel.Objects {
		objs = append(objs, map[string]any{"objectType": o.ObjectType, "publicId": o.ObjectPublicID, "revisionNo": o.RevisionNo})
	}
	deps := make([]map[string]string, 0, len(rel.Dependencies))
	for _, d := range rel.Dependencies {
		deps = append(deps, map[string]string{"dependencyCode": d.DependencyCode, "dependencyVersion": d.DependencyVersion})
	}
	return map[string]any{
		"package": rel.PackageCode, "version": rel.Version,
		"publishedAt": rel.PublishedAt.UTC().Format(time.RFC3339Nano),
		"dependencies": deps, "objects": objs,
	}
}

func writeResponse(data map[string]any, cs *domain.ChangeSet) map[string]any {
	out := map[string]any{"data": data}
	if cs != nil {
		out["changeSet"] = changeSetDTO(cs)
	}
	return out
}

func entityDTO(e *domain.Entity) map[string]any {
	return map[string]any{
		"id": e.PublicID, "canonicalId": e.ID.String(), "status": e.Status,
		"revisionNo": e.RevisionNo,
		"labels": e.Labels, "descriptions": e.Descriptions,
		"createdAt": e.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updatedAt": e.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func propertyDTO(p *domain.Property) map[string]any {
	return map[string]any{
		"id": p.PublicID, "canonicalId": p.ID.String(), "datatype": p.Datatype, "status": p.Status,
		"revisionNo": p.RevisionNo,
		"labels": p.Labels, "descriptions": p.Descriptions,
		"createdAt": p.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updatedAt": p.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func statementDTO(st *domain.Statement) map[string]any {
	out := map[string]any{
		"id": st.PublicID, "canonicalId": st.ID.String(),
		"subject": st.SubjectQID, "property": st.PropertyPID,
		"status": st.Status, "revisionNo": st.RevisionNo, "value": st.Value,
		"createdAt": st.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updatedAt": st.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	if len(st.Qualifiers) > 0 {
		quals := make([]map[string]any, 0, len(st.Qualifiers))
		for _, q := range st.Qualifiers {
			quals = append(quals, map[string]any{"property": q.PropertyPID, "value": q.Value})
		}
		out["qualifiers"] = quals
	}
	if len(st.ReferenceIDs) > 0 {
		out["referenceIds"] = st.ReferenceIDs
	}
	if st.ValidFrom != nil {
		out["validFrom"] = st.ValidFrom.UTC().Format(time.RFC3339Nano)
	}
	if st.ValidTo != nil {
		out["validTo"] = st.ValidTo.UTC().Format(time.RFC3339Nano)
	}
	return out
}

func referenceDTO(ref *domain.Reference) map[string]any {
	return map[string]any{
		"id": ref.PublicID, "canonicalId": ref.ID.String(),
		"fields": ref.Fields,
		"createdAt": ref.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func changeSetDTO(cs *domain.ChangeSet) map[string]any {
	items := make([]map[string]any, 0, len(cs.Items))
	for _, it := range cs.Items {
		items = append(items, map[string]any{
			"objectType": it.ObjectType,
			"objectId":   it.ObjectID.String(),
			"publicId":   it.PublicID,
			"op":         it.Op,
		})
	}
	return map[string]any{
		"id": cs.PublicID, "canonicalId": cs.ID.String(),
		"actor": cs.Actor, "operationType": cs.OperationType,
		"committedAt": cs.CommittedAt.UTC().Format(time.RFC3339Nano),
		"items": items,
	}
}

func entityRevisionDTO(rev *domain.EntityRevision) map[string]any {
	return map[string]any{
		"revisionNo": rev.RevisionNo, "status": rev.Status,
		"labels": rev.Labels, "descriptions": rev.Descriptions,
		"actor": rev.Actor,
		"createdAt": rev.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func statementRevisionDTO(rev *domain.StatementRevision) map[string]any {
	out := map[string]any{
		"revisionNo": rev.RevisionNo, "status": rev.Status, "value": rev.Value,
		"actor": rev.Actor,
		"createdAt": rev.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
	if len(rev.Qualifiers) > 0 {
		quals := make([]map[string]any, 0, len(rev.Qualifiers))
		for _, q := range rev.Qualifiers {
			quals = append(quals, map[string]any{"property": q.PropertyPID, "value": q.Value})
		}
		out["qualifiers"] = quals
	}
	if len(rev.ReferenceIDs) > 0 {
		out["referenceIds"] = rev.ReferenceIDs
	}
	return out
}

func writeEngineError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, engine.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, engine.ErrInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, engine.ErrConflict):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, engine.ErrIdempotencyConflict):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, engine.ErrReleaseImmutable):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, engine.ErrForbidden):
		writeError(w, http.StatusForbidden, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	code := "error"
	switch status {
	case http.StatusBadRequest:
		code = "invalid"
	case http.StatusUnauthorized:
		code = "unauthenticated"
	case http.StatusForbidden:
		code = "forbidden"
	case http.StatusNotFound:
		code = "not_found"
	case http.StatusConflict:
		code = "conflict"
	case http.StatusServiceUnavailable:
		code = "unavailable"
	}
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"code":    code,
			"message": msg,
		},
	})
}
