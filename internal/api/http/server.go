package apihttp

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/l-ra/knowledge-core/internal/auth"
	"github.com/l-ra/knowledge-core/internal/config"
	"github.com/l-ra/knowledge-core/internal/datatype"
	"github.com/l-ra/knowledge-core/internal/domain"
	"github.com/l-ra/knowledge-core/internal/engine"
	"github.com/l-ra/knowledge-core/internal/metrics"
	"github.com/l-ra/knowledge-core/internal/pkgcompat"
	"github.com/l-ra/knowledge-core/internal/store"
	"github.com/l-ra/knowledge-core/web/ui"
)

type Server struct {
	engine *engine.Engine
	store  *store.Store
	authn  *SwitchableAuthenticator
	cfgMu  sync.RWMutex
	cfg    config.Config
}

func New(eng *engine.Engine, st *store.Store, authn Authenticator, cfg config.Config) http.Handler {
	sw, ok := authn.(*SwitchableAuthenticator)
	if !ok {
		sw = NewSwitchableAuthenticator(authn)
	}
	s := &Server{engine: eng, store: st, authn: sw, cfg: cfg}
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
		r.Use(authMiddleware(sw))
		r.Get("/me", s.me)

		r.Get("/admin/auth", s.getAdminAuth)
		r.Put("/admin/auth", s.putAdminAuth)

		r.Get("/entities", s.listEntities)
		r.Post("/entities", s.createEntity)
		r.Get("/entities/facets", s.listEntityFacets)
		r.Post("/entities/batch-read", s.batchReadEntities)
		r.Get("/entities/{qid}/validation", s.getEntityValidation)
		r.Get("/entities/{qid}/history", s.getEntityHistory)
		r.Get("/entities/{qid}/statements", s.listEntityStatements)
		r.Get("/entities/{qid}/incoming", s.listIncomingStatements)
		r.Get("/entities/{qid}/graph", s.getEntityGraph)
		r.Post("/entities/{qid}/move", s.moveEntity)
		r.Post("/entities/{qid}/deprecate", s.deprecateEntity)
		r.Post("/entities/{qid}/delete", s.deleteEntity)
		r.Get("/entities/{qid}", s.getEntity)
		r.Patch("/entities/{qid}", s.updateEntity)
		r.Put("/entities/{qid}/iri-aliases", s.putEntityIRIAliases)

		r.Get("/properties", s.listProperties)
		r.Post("/properties", s.createProperty)
		r.Get("/properties/{pid}", s.getProperty)
		r.Patch("/properties/{pid}", s.patchProperty)

		r.Get("/packages", s.listPackages)
		r.Post("/packages", s.createPackage)
		r.Get("/packages/{code}", s.getPackage)
		r.Patch("/packages/{code}", s.updatePackage)
		r.Delete("/packages/{code}", s.deletePackage)
		r.Post("/packages/{code}/rdf/import", s.importPackageRDF)
		r.Get("/packages/{code}/objects", s.listPackageObjects)
		r.Get("/packages/{code}/releases", s.listPackageReleases)
		r.Post("/packages/{code}/releases", s.publishRelease)
		r.Get("/packages/{code}/releases/{version}", s.getRelease)
		r.Get("/packages/{code}/releases/{version}/bundle", s.exportReleaseBundle)
		r.Post("/packages/{code}/releases/{version}/mutate", s.mutateRelease)
		r.Post("/releases/import", s.importRelease)
		r.Post("/rdf/analyze", s.analyzeRDF)
		r.Post("/rdf/prefixes", s.parseRDFPrefixes)
		r.Post("/rdf/import", s.importRDFGlobal)

		r.Get("/objects/{id}/releases", s.listObjectReleases)

		r.Post("/references", s.createReference)
		r.Get("/references/{rid}", s.getReference)

		r.Get("/statements", s.listStatements)
		r.Post("/statements", s.createStatement)
		r.Get("/statements/{sid}", s.getStatement)
		r.Post("/statements/{sid}/revise", s.reviseStatement)
		r.Post("/statements/{sid}/deprecate", s.deprecateStatement)
		r.Get("/statements/{sid}/history", s.getStatementHistory)

		r.Get("/changesets", s.listChangeSets)
		r.Post("/changesets", s.applyChangeSet)
		r.Post("/changesets/open", s.openChangeSet)
		r.Post("/changesets/{cid}/commit", s.commitOpenChangeSet)
		r.Post("/changesets/{cid}/cancel", s.cancelOpenChangeSet)
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

		r.Get("/admin/schema-config", s.getSchemaConfig)
		r.Put("/admin/schema-config", s.putSchemaConfig)

		r.Post("/validation/reports", s.createValidationReport)
		r.Get("/validation/reports/{id}", s.getValidationReport)

		r.Get("/classes", s.listClasses)
		r.Post("/classes", s.createClass)
		r.Get("/classes/{cid}", s.getClass)

		r.Get("/shapes", s.listShapes)
		r.Post("/shapes", s.createShape)
		r.Get("/shapes/{code}", s.getShape)

		r.Post("/projections/outbox/process", s.processOutbox)
		r.Post("/projections/search/rebuild", s.rebuildSearchProjection)
		r.Get("/projections/search", s.searchProjection)
		r.Post("/projections/rdf/rebuild", s.rebuildRDFProjection)
		r.Get("/projections/rdf", s.exportRDF)
	})

	mountUI(r)

	return r
}

func (s *Server) liveConfig() config.Config {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return s.cfg
}

func (s *Server) setLiveConfig(cfg config.Config) {
	s.cfgMu.Lock()
	s.cfg = cfg
	s.cfgMu.Unlock()
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
	fileServer := http.StripPrefix("/ui", http.FileServer(http.FS(sub)))
	r.Get("/ui", func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, "/ui/", http.StatusFound)
	})
	r.Handle("/ui/*", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		rel := strings.TrimPrefix(req.URL.Path, "/ui/")
		if rel == "" || rel == "/" {
			req.URL.Path = "/ui/"
			fileServer.ServeHTTP(w, req)
			return
		}
		// SPA fallback: unknown paths (e.g. /ui/callback) must serve index.html.
		// Keep the /ui prefix so StripPrefix still matches.
		if _, err := sub.Open(rel); err != nil {
			req.URL.Path = "/ui/"
			fileServer.ServeHTTP(w, req)
			return
		}
		fileServer.ServeHTTP(w, req)
	}))
}

func subjectFromRequest(r *http.Request) (auth.Subject, bool) {
	return auth.SubjectFromContext(r.Context())
}

func pathParam(r *http.Request, key string) string {
	raw := chi.URLParam(r, key)
	if raw == "" {
		return ""
	}
	decoded, err := url.PathUnescape(raw)
	if err != nil {
		return raw
	}
	return decoded
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
	IRILocal     string            `json:"iriLocal,omitempty"`
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
		PackageCode: req.PackageCode, Labels: req.Labels, Descriptions: req.Descriptions, IRILocal: req.IRILocal,
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
	r = withActiveChangeSets(r)
	ent, err := s.engine.GetEntity(r.Context(), pathParam(r, "qid"))
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, entityDTO(ent))
}

type updateEntityReq struct {
	Labels           map[string]string `json:"labels"`
	Descriptions     map[string]string `json:"descriptions"`
	IRILocal         *string           `json:"iriLocal"`
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
	res, err := s.engine.UpdateEntity(r.Context(), meta, pathParam(r, "qid"), domain.UpdateEntityInput{
		Labels: req.Labels, Descriptions: req.Descriptions, IRILocal: req.IRILocal, ExpectedRevision: req.ExpectedRevision,
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

type expectedRevisionReq struct {
	ExpectedRevision int `json:"expectedRevision"`
}

func (s *Server) deprecateEntity(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	var req expectedRevisionReq
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	meta := writeMetaFromRequest(r, "deprecateEntity", hashBody(body))
	res, err := s.engine.DeprecateEntity(r.Context(), meta, pathParam(r, "qid"), req.ExpectedRevision)
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, writeResponse(entityDTO(&res.Value), res.ChangeSet))
}

func (s *Server) deleteEntity(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	var req expectedRevisionReq
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	meta := writeMetaFromRequest(r, "deleteEntity", hashBody(body))
	res, err := s.engine.DeleteEntity(r.Context(), meta, pathParam(r, "qid"), req.ExpectedRevision)
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, writeResponse(entityDTO(&res.Value), res.ChangeSet))
}

type putEntityIRIAliasesReq struct {
	Aliases []struct {
		IRI  string `json:"iri"`
		Kind string `json:"kind"`
	} `json:"aliases"`
}

func (s *Server) putEntityIRIAliases(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	var req putEntityIRIAliasesReq
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	aliases := make([]domain.EntityIRIAlias, 0, len(req.Aliases))
	for _, a := range req.Aliases {
		aliases = append(aliases, domain.EntityIRIAlias{IRI: a.IRI, Kind: a.Kind})
	}
	meta := writeMetaFromRequest(r, "setEntityIRIAliases", hashBody(body))
	res, err := s.engine.SetEntityIRIAliases(r.Context(), meta, pathParam(r, "qid"), aliases)
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, writeResponse(entityDTO(&res.Value), res.ChangeSet))
}

func (s *Server) getEntityHistory(w http.ResponseWriter, r *http.Request) {
	h, err := s.engine.GetEntityHistory(r.Context(), pathParam(r, "qid"))
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
	PackageCode  string                     `json:"packageCode,omitempty"`
	Datatype     string                     `json:"datatype"`
	Labels       map[string]string          `json:"labels"`
	Descriptions map[string]string          `json:"descriptions"`
	Constraints  domain.PropertyConstraints `json:"constraints,omitempty"`
	IRILocal     string                     `json:"iriLocal,omitempty"`
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
		PackageCode: req.PackageCode, Datatype: dt, Labels: req.Labels,
		Descriptions: req.Descriptions, Constraints: req.Constraints, IRILocal: req.IRILocal,
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
	p, err := s.engine.GetProperty(r.Context(), pathParam(r, "pid"))
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
	Upsert       bool                    `json:"upsert,omitempty"`
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
		ValidFrom: req.ValidFrom, ValidTo: req.ValidTo, Upsert: req.Upsert,
	})
	if err != nil {
		writeEngineError(w, err)
		return
	}
	status := http.StatusCreated
	if res.Replay {
		status = http.StatusOK
	}
	writeJSON(w, status, writeResponse(statementDTO(&res.Value), res.ChangeSet, res.Validation))
}

type reviseStatementReq struct {
	ExpectedRevision int                      `json:"expectedRevision"`
	Value            *datatype.Value          `json:"value,omitempty"`
	Qualifiers       *[]domain.QualifierInput `json:"qualifiers,omitempty"`
	ReferenceIDs     *[]string                `json:"referenceIds,omitempty"`
	ValidFrom        *time.Time               `json:"validFrom,omitempty"`
	ValidTo          *time.Time               `json:"validTo,omitempty"`
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
	if rejectIfOpenChangeSet(w, meta, "createReference") {
		return
	}
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
	ref, err := s.engine.GetReference(r.Context(), pathParam(r, "rid"))
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
	res, err := s.engine.ReviseStatement(r.Context(), meta, pathParam(r, "sid"), in)
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, writeResponse(statementDTO(&res.Value), res.ChangeSet))
}

func (s *Server) deprecateStatement(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	var req expectedRevisionReq
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	meta := writeMetaFromRequest(r, "deprecateStatement", hashBody(body))
	res, err := s.engine.DeprecateStatement(r.Context(), meta, pathParam(r, "sid"), req.ExpectedRevision)
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, writeResponse(statementDTO(&res.Value), res.ChangeSet))
}

func (s *Server) getStatement(w http.ResponseWriter, r *http.Request) {
	r = withActiveChangeSets(r)
	st, err := s.engine.GetStatement(r.Context(), pathParam(r, "sid"))
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, statementDTO(st))
}

func (s *Server) getStatementHistory(w http.ResponseWriter, r *http.Request) {
	h, err := s.engine.GetStatementHistory(r.Context(), pathParam(r, "sid"))
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

func (s *Server) listStatements(w http.ResponseWriter, r *http.Request) {
	r = withActiveChangeSets(r)
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	list, next, err := s.engine.ListStatements(r.Context(), store.StatementListOptions{
		Limit:       limit,
		Cursor:      r.URL.Query().Get("cursor"),
		SubjectQID:  r.URL.Query().Get("subject"),
		PropertyPID: r.URL.Query().Get("property"),
		ObjectQID:   r.URL.Query().Get("object"),
	})
	if err != nil {
		writeEngineError(w, err)
		return
	}
	out := make([]any, 0, len(list))
	for i := range list {
		out = append(out, statementDTO(&list[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "nextCursor": next})
}

func (s *Server) listEntityStatements(w http.ResponseWriter, r *http.Request) {
	r = withActiveChangeSets(r)
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	list, next, err := s.engine.ListStatements(r.Context(), store.StatementListOptions{
		Limit:       limit,
		Cursor:      r.URL.Query().Get("cursor"),
		SubjectQID:  pathParam(r, "qid"),
		PropertyPID: r.URL.Query().Get("property"),
	})
	if err != nil {
		writeEngineError(w, err)
		return
	}
	out := make([]any, 0, len(list))
	for i := range list {
		out = append(out, statementDTO(&list[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"statements": out, "nextCursor": next})
}

func (s *Server) listIncomingStatements(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	list, next, err := s.engine.ListStatements(r.Context(), store.StatementListOptions{
		Limit:       limit,
		Cursor:      r.URL.Query().Get("cursor"),
		ObjectQID:   pathParam(r, "qid"),
		PropertyPID: r.URL.Query().Get("property"),
	})
	if err != nil {
		writeEngineError(w, err)
		return
	}
	out := make([]any, 0, len(list))
	for i := range list {
		out = append(out, statementDTO(&list[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"statements": out, "nextCursor": next})
}

func (s *Server) getEntityGraph(w http.ResponseWriter, r *http.Request) {
	depth := 1
	if v := r.URL.Query().Get("depth"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			depth = n
		}
	}
	g, err := s.engine.GetEntityGraph(r.Context(), pathParam(r, "qid"), depth)
	if err != nil {
		writeEngineError(w, err)
		return
	}
	neighbors := make([]any, 0, len(g.Neighbors))
	for i := range g.Neighbors {
		neighbors = append(neighbors, entityDTO(&g.Neighbors[i]))
	}
	outSt := make([]any, 0, len(g.Outgoing))
	for i := range g.Outgoing {
		outSt = append(outSt, statementDTO(&g.Outgoing[i]))
	}
	inSt := make([]any, 0, len(g.Incoming))
	for i := range g.Incoming {
		inSt = append(inSt, statementDTO(&g.Incoming[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"entity":    entityDTO(&g.Entity),
		"outgoing":  outSt,
		"incoming":  inSt,
		"neighbors": neighbors,
	})
}

type moveEntityReq struct {
	PackageCode      string `json:"packageCode"`
	ExpectedRevision int    `json:"expectedRevision"`
}

func (s *Server) moveEntity(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	var req moveEntityReq
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	meta := writeMetaFromRequest(r, "moveEntity", hashBody(body))
	res, err := s.engine.MoveEntity(r.Context(), meta, pathParam(r, "qid"), domain.MoveEntityInput{
		PackageCode: req.PackageCode, ExpectedRevision: req.ExpectedRevision,
	})
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, writeResponse(entityDTO(&res.Value), res.ChangeSet))
}

type patchPropertyReq struct {
	Constraints      *domain.PropertyConstraints `json:"constraints"`
	ExpectedRevision int                         `json:"expectedRevision"`
}

func (s *Server) patchProperty(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	var req patchPropertyReq
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	meta := writeMetaFromRequest(r, "updateProperty", hashBody(body))
	res, err := s.engine.UpdateProperty(r.Context(), meta, pathParam(r, "pid"), domain.UpdatePropertyInput{
		Constraints: req.Constraints, ExpectedRevision: req.ExpectedRevision,
	})
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, writeResponse(propertyDTO(&res.Value), res.ChangeSet))
}

type applyChangeSetReq struct {
	OperationType string                   `json:"operationType"`
	Comment       string                   `json:"comment"`
	Operations    []domain.ChangeOperation `json:"operations"`
}

func (s *Server) applyChangeSet(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		if err == errPayloadTooLarge {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{
				"error": map[string]any{"code": "payload_too_large", "message": "request body exceeds 4 MiB"},
			})
			return
		}
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
	// Q2: { data: { results }, changeSet: DTO }
	var results any
	if len(res.ResponseRaw) > 0 {
		var parsed map[string]any
		if json.Unmarshal(res.ResponseRaw, &parsed) == nil {
			results = parsed["results"]
		}
	}
	if results == nil {
		results = []any{}
	}
	status := http.StatusOK
	if res.Replay {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{
		"data":      map[string]any{"results": results},
		"changeSet": changeSetDTO(&res.Value),
	})
}

func (s *Server) listChangeSets(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	opt := store.ChangeSetListOptions{
		Query:         q.Get("q"),
		Cursor:        q.Get("cursor"),
		Actor:         q.Get("actor"),
		OperationType: q.Get("operationType"),
		CorrelationID: q.Get("correlationId"),
		ObjectID:      q.Get("objectId"),
		Status:        q.Get("status"),
		Limit:         50,
	}
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			opt.Limit = n
		}
	}
	if v := strings.TrimSpace(q.Get("committedFrom")); v != "" {
		ts, err := time.Parse(time.RFC3339Nano, v)
		if err != nil {
			ts, err = time.Parse(time.RFC3339, v)
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid committedFrom")
			return
		}
		opt.CommittedFrom = &ts
	}
	if v := strings.TrimSpace(q.Get("committedTo")); v != "" {
		ts, err := time.Parse(time.RFC3339Nano, v)
		if err != nil {
			ts, err = time.Parse(time.RFC3339, v)
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid committedTo")
			return
		}
		opt.CommittedTo = &ts
	}
	items, next, err := s.engine.ListChangeSets(r.Context(), opt)
	if err != nil {
		writeEngineError(w, err)
		return
	}
	out := make([]any, 0, len(items))
	for i := range items {
		out = append(out, changeSetSummaryDTO(&items[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "nextCursor": next})
}

func (s *Server) getChangeSet(w http.ResponseWriter, r *http.Request) {
	cs, err := s.engine.GetChangeSet(r.Context(), pathParam(r, "cid"))
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, changeSetDTO(cs))
}

type createPackageReq struct {
	Code         string                     `json:"code"`
	Lifecycle    string                     `json:"lifecycle"`
	IRIBase      string                     `json:"iriBase,omitempty"`
	Labels       map[string]string          `json:"labels"`
	Descriptions map[string]string          `json:"descriptions,omitempty"`
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
	if rejectIfOpenChangeSet(w, meta, "createPackage") {
		return
	}
	res, err := s.engine.CreatePackage(r.Context(), meta, domain.CreatePackageInput{
		Code: req.Code, Lifecycle: lc, IRIBase: req.IRIBase, Labels: req.Labels,
		Descriptions: req.Descriptions, Dependencies: req.Dependencies,
	})
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, writeResponse(packageDTO(&res.Value), res.ChangeSet))
}

type updatePackageReq struct {
	IRIBase *string           `json:"iriBase"`
	Labels  map[string]string `json:"labels"`
}

func (s *Server) updatePackage(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	var req updatePackageReq
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	meta := writeMetaFromRequest(r, "updatePackage", hashBody(body))
	if rejectIfOpenChangeSet(w, meta, "updatePackage") {
		return
	}
	res, err := s.engine.UpdatePackage(r.Context(), meta, pathParam(r, "code"), domain.UpdatePackageInput{
		IRIBase: req.IRIBase, Labels: req.Labels,
	})
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, writeResponse(packageDTO(&res.Value), res.ChangeSet))
}

func (s *Server) deletePackage(w http.ResponseWriter, r *http.Request) {
	meta := writeMetaFromRequest(r, "deletePackage", "")
	if rejectIfOpenChangeSet(w, meta, "deletePackage") {
		return
	}
	res, err := s.engine.DeletePackage(r.Context(), meta, pathParam(r, "code"))
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, writeResponse(packageDTO(&res.Value), res.ChangeSet))
}

type importPackageRDFReq struct {
	NTriples string `json:"ntriples"`
	DryRun   bool   `json:"dryRun"`
}

func (s *Server) importPackageRDF(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	var req importPackageRDFReq
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if strings.TrimSpace(req.NTriples) == "" {
		writeError(w, http.StatusBadRequest, "ntriples required")
		return
	}
	meta := writeMetaFromRequest(r, "importRDF", hashBody(body))
	if rejectIfOpenChangeSet(w, meta, "importRDF") {
		return
	}
	res, err := s.engine.ImportRDF(r.Context(), meta, pathParam(r, "code"), domain.RDFImportInput{
		NTriples: req.NTriples, DryRun: req.DryRun,
	})
	if err != nil {
		writeEngineError(w, err)
		return
	}
	status := http.StatusOK
	if !req.DryRun && res.ChangeSetID != "" {
		status = http.StatusCreated
	}
	writeJSON(w, status, res)
}

func (s *Server) analyzeRDF(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	var req struct {
		NTriples       string `json:"ntriples"`
		TurtlePrefixes string `json:"turtlePrefixes"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if strings.TrimSpace(req.NTriples) == "" && strings.TrimSpace(req.TurtlePrefixes) == "" {
		writeError(w, http.StatusBadRequest, "ntriples or turtlePrefixes required")
		return
	}
	res, err := s.engine.AnalyzeRDF(r.Context(), req.NTriples, req.TurtlePrefixes)
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) parseRDFPrefixes(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	var req struct {
		TurtlePrefixes string `json:"turtlePrefixes"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if strings.TrimSpace(req.TurtlePrefixes) == "" {
		writeError(w, http.StatusBadRequest, "turtlePrefixes required")
		return
	}
	res, err := s.engine.ParseTurtlePrefixes(r.Context(), req.TurtlePrefixes)
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) importRDFGlobal(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	var req struct {
		NTriples    string                       `json:"ntriples"`
		DryRun      bool                         `json:"dryRun"`
		Assignments []domain.RDFGlobalAssignment `json:"assignments"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	meta := writeMetaFromRequest(r, "importRDFGlobal", hashBody(body))
	if rejectIfOpenChangeSet(w, meta, "importRDFGlobal") {
		return
	}
	res, err := s.engine.ImportRDFGlobal(r.Context(), meta, domain.RDFGlobalImportInput{
		NTriples: req.NTriples, DryRun: req.DryRun, Assignments: req.Assignments,
	})
	if err != nil {
		writeEngineError(w, err)
		return
	}
	status := http.StatusOK
	if !req.DryRun {
		status = http.StatusCreated
	}
	writeJSON(w, status, res)
}

func (s *Server) getPackage(w http.ResponseWriter, r *http.Request) {
	pkg, err := s.engine.GetPackage(r.Context(), pathParam(r, "code"))
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
	if rejectIfOpenChangeSet(w, meta, "publishRelease") {
		return
	}
	res, err := s.engine.PublishRelease(r.Context(), meta, pathParam(r, "code"), domain.PublishReleaseInput{
		Version: req.Version,
	})
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, writeResponse(releaseDTO(&res.Value), res.ChangeSet))
}

func (s *Server) getRelease(w http.ResponseWriter, r *http.Request) {
	rel, err := s.engine.GetRelease(r.Context(), pathParam(r, "code"), pathParam(r, "version"))
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, releaseDTO(rel))
}

func (s *Server) exportReleaseBundle(w http.ResponseWriter, r *http.Request) {
	bundle, err := s.engine.ExportReleaseBundle(r.Context(), pathParam(r, "code"), pathParam(r, "version"))
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, bundle)
}

func (s *Server) mutateRelease(w http.ResponseWriter, r *http.Request) {
	meta := writeMetaFromRequest(r, "mutateRelease", "")
	if rejectIfOpenChangeSet(w, meta, "mutateRelease") {
		return
	}
	err := s.engine.MutateRelease(r.Context(), pathParam(r, "code"), pathParam(r, "version"))
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
	if rejectIfOpenChangeSet(w, meta, "importRelease") {
		return
	}
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
	out := map[string]any{
		"code": p.Code, "lifecycle": p.Lifecycle, "labels": p.Labels,
		"iriBase":      p.IRIBase,
		"dependencies": deps,
		"createdAt":    p.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updatedAt":    p.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	if p.RootEntityID != "" {
		out["rootEntityId"] = p.RootEntityID
	}
	if len(p.Descriptions) > 0 {
		out["descriptions"] = p.Descriptions
	}
	if p.LatestReleaseVersion != "" {
		out["latestReleaseVersion"] = p.LatestReleaseVersion
	}
	if p.ModifiedAfterRelease {
		out["modifiedAfterRelease"] = true
	}
	return out
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
	objectCount := len(rel.Objects)
	if objectCount == 0 {
		objectCount = rel.ObjectCount
	}
	return map[string]any{
		"package": rel.PackageCode, "version": rel.Version,
		"publishedAt":  rel.PublishedAt.UTC().Format(time.RFC3339Nano),
		"dependencies": deps, "objects": objs, "objectCount": objectCount,
	}
}

func writeResponse(data map[string]any, cs *domain.ChangeSet, validation ...*domain.ValidationResult) map[string]any {
	out := map[string]any{"data": data}
	if cs != nil {
		out["changeSet"] = changeSetDTO(cs)
	}
	if len(validation) > 0 && validation[0] != nil {
		out["validation"] = validationDTO(validation[0])
	}
	return out
}

func entityDTO(e *domain.Entity) map[string]any {
	kind := string(e.Kind)
	if kind == "" {
		kind = string(domain.EntityKindEntity)
	}
	out := map[string]any{
		"id": e.PublicID, "canonicalId": e.ID.String(), "status": e.Status,
		"kind": kind, "revisionNo": e.RevisionNo,
		"labels": e.Labels, "descriptions": e.Descriptions,
		"displayId": datatype.PackageDisplayID(e.PackageCode, e.IRILocal, e.PublicID),
		"createdAt": e.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updatedAt": e.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	if e.PackageCode != "" {
		out["packageCode"] = e.PackageCode
	}
	out["iriLocal"] = e.IRILocal
	if e.IRI != "" {
		out["iri"] = e.IRI
	}
	aliases := make([]map[string]string, 0, len(e.IRIAliases))
	for _, a := range e.IRIAliases {
		aliases = append(aliases, map[string]string{"iri": a.IRI, "kind": a.Kind})
	}
	out["iriAliases"] = aliases
	if e.PropertyProfile != nil {
		out["propertyProfile"] = map[string]any{
			"datatype":    e.PropertyProfile.Datatype,
			"constraints": e.PropertyProfile.Constraints,
		}
	}
	if e.ClassProfile != nil {
		out["classProfile"] = map[string]any{
			"subClassOf": e.ClassProfile.SubClassOf,
		}
	}
	if len(e.EffectiveClasses) > 0 {
		out["effectiveClasses"] = e.EffectiveClasses
	}
	return out
}

func propertyDTO(p *domain.Property) map[string]any {
	out := map[string]any{
		"id": p.PublicID, "canonicalId": p.ID.String(), "datatype": p.Datatype, "status": p.Status,
		"revisionNo": p.RevisionNo,
		"labels":     p.Labels, "descriptions": p.Descriptions,
		"constraints": p.Constraints,
		"displayId":   datatype.PackageDisplayID(p.PackageCode, p.IRILocal, p.PublicID),
		"createdAt":   p.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updatedAt":   p.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	if p.PackageCode != "" {
		out["packageCode"] = p.PackageCode
	}
	out["iriLocal"] = p.IRILocal
	if p.IRI != "" {
		out["iri"] = p.IRI
	}
	return out
}

func statementDTO(st *domain.Statement) map[string]any {
	out := map[string]any{
		"id": st.PublicID, "canonicalId": st.ID.String(),
		"subject": st.SubjectQID, "property": st.PropertyPID,
		"displayId": datatype.PackageDisplayID(st.PackageCode, "", st.PublicID),
		"status":    st.Status, "revisionNo": st.RevisionNo, "value": st.Value,
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
		"displayId": datatype.PackageDisplayID("", "", ref.PublicID),
		"fields":    ref.Fields,
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
	status := string(cs.Status)
	if status == "" {
		status = string(domain.ChangeSetCommitted)
	}
	out := map[string]any{
		"id": cs.PublicID, "canonicalId": cs.ID.String(),
		"displayId": datatype.PackageDisplayID("", "", cs.PublicID),
		"actor":     cs.Actor, "operationType": cs.OperationType,
		"status":    status,
		"itemCount": cs.ItemCount,
		"items":     items,
	}
	if !cs.OpenedAt.IsZero() {
		out["openedAt"] = cs.OpenedAt.UTC().Format(time.RFC3339Nano)
	}
	if !cs.CommittedAt.IsZero() {
		out["committedAt"] = cs.CommittedAt.UTC().Format(time.RFC3339Nano)
	}
	if cs.Comment != "" {
		out["comment"] = cs.Comment
	}
	if cs.CorrelationID != "" {
		out["correlationId"] = cs.CorrelationID
	}
	if cs.IdempotencyKey != "" {
		out["idempotencyKey"] = cs.IdempotencyKey
	}
	if len(cs.Claims) > 0 {
		claims := make([]map[string]any, 0, len(cs.Claims))
		for _, c := range cs.Claims {
			claims = append(claims, map[string]any{
				"objectType":     c.ObjectType,
				"objectId":       c.ObjectID.String(),
				"canonicalIri":   c.CanonicalIRI,
				"baseRevisionNo": c.BaseRevisionNo,
				"opKind":         c.OpKind,
			})
		}
		out["claims"] = claims
	}
	return out
}

func changeSetSummaryDTO(cs *domain.ChangeSet) map[string]any {
	out := changeSetDTO(cs)
	delete(out, "items")
	return out
}

func entityRevisionDTO(rev *domain.EntityRevision) map[string]any {
	return map[string]any{
		"revisionNo": rev.RevisionNo, "status": rev.Status,
		"labels": rev.Labels, "descriptions": rev.Descriptions,
		"actor":     rev.Actor,
		"createdAt": rev.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func statementRevisionDTO(rev *domain.StatementRevision) map[string]any {
	out := map[string]any{
		"revisionNo": rev.RevisionNo, "status": rev.Status, "value": rev.Value,
		"actor":     rev.Actor,
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
	var vf engine.ValidationFailure
	if errors.As(err, &vf) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error":      map[string]any{"code": "validation", "message": vf.Error()},
			"validation": validationDTO(&vf.Result),
		})
		return
	}
	var br *pkgcompat.BreakingError
	if errors.As(err, &br) {
		findings := make([]map[string]any, 0, len(br.Findings))
		for _, f := range br.Findings {
			if f.Kind != pkgcompat.KindBreaking {
				continue
			}
			findings = append(findings, map[string]any{
				"kind":           f.Kind,
				"reasonCode":     f.ReasonCode,
				"objectType":     f.ObjectType,
				"objectPublicId": f.ObjectPublicID,
				"detail":         f.Detail,
			})
		}
		writeJSON(w, http.StatusConflict, map[string]any{
			"error": map[string]any{
				"code":    "compat_breaking",
				"message": br.Error(),
			},
			"findings": findings,
		})
		return
	}
	switch {
	case errors.Is(err, engine.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, engine.ErrUnsupportedInOpenChangeSet):
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]any{
				"code":    "unsupported_in_open_changeset",
				"message": err.Error(),
			},
		})
	case errors.Is(err, engine.ErrInvalid):
		msg := err.Error()
		code := "invalid"
		if strings.Contains(msg, "batch limit exceeded") {
			code = "batch_limit_exceeded"
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]any{"code": code, "message": msg},
		})
		return
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
