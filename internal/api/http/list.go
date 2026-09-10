package apihttp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/l-ra/knowledge-core/internal/domain"
	"github.com/l-ra/knowledge-core/internal/engine"
	"github.com/l-ra/knowledge-core/internal/store"
)

func (s *Server) listEntities(w http.ResponseWriter, r *http.Request) {
	r = withActiveChangeSets(r)
	q := r.URL.Query()
	expand, err := engine.ParseListExpand(q.Get("include"), q.Get("properties"))
	if err != nil {
		writeEngineError(w, err)
		return
	}
	opt := store.ListOptions{
		Query:             q.Get("q"),
		Cursor:            q.Get("cursor"),
		Kind:              q.Get("kind"),
		PackageCode:       q.Get("package"),
		IRILocal:          q.Get("iriLocal"),
		IRI:               q.Get("iri"),
		InstanceOf:        q.Get("instanceOf"),
		IncludeSubclasses: q.Get("includeSubclasses") == "true" || q.Get("includeSubclasses") == "1",
		Limit:             50,
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
	if expand.EffectiveClasses {
		if err := s.engine.AttachEffectiveClasses(r.Context(), items); err != nil {
			writeEngineError(w, err)
			return
		}
	}
	var stmtsBySubj map[string][]domain.Statement
	if expand.Statements {
		qids := make([]string, len(items))
		for i := range items {
			qids[i] = items[i].PublicID
		}
		stmtsBySubj, err = s.engine.StatementsBySubjects(r.Context(), qids, expand.Properties)
		if err != nil {
			writeEngineError(w, err)
			return
		}
	}
	out := make([]any, 0, len(items))
	for i := range items {
		dto := entityDTO(&items[i])
		if expand.Statements {
			sts := stmtsBySubj[items[i].PublicID]
			arr := make([]any, 0, len(sts))
			for j := range sts {
				arr = append(arr, statementDTO(&sts[j]))
			}
			dto["statements"] = arr
		}
		out = append(out, dto)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "nextCursor": next})
}

func (s *Server) listEntityFacets(w http.ResponseWriter, r *http.Request) {
	r = withActiveChangeSets(r)
	q := r.URL.Query()
	groupBy := strings.TrimSpace(q.Get("groupBy"))
	if groupBy == "" {
		groupBy = "instanceOf"
	}
	if groupBy != "instanceOf" {
		writeEngineError(w, fmt.Errorf("%w: unsupported groupBy", engine.ErrInvalid))
		return
	}
	facets, err := s.engine.CountEntityFacetsByInstanceOf(r.Context(), q.Get("package"))
	if err != nil {
		writeEngineError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(facets))
	for _, f := range facets {
		out = append(out, map[string]any{"classId": f.ClassID, "count": f.Count})
	}
	writeJSON(w, http.StatusOK, map[string]any{"facets": out})
}

func (s *Server) batchReadEntities(w http.ResponseWriter, r *http.Request) {
	r = withActiveChangeSets(r)
	var body struct {
		IDs        []string `json:"ids"`
		Include    []string `json:"include"`
		Properties []string `json:"properties"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeEngineError(w, fmt.Errorf("%w: invalid json body", engine.ErrInvalid))
		return
	}
	results, err := s.engine.BatchReadEntities(r.Context(), engine.BatchReadInput{
		IDs:        body.IDs,
		Include:    body.Include,
		Properties: body.Properties,
	})
	if err != nil {
		writeEngineError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(results))
	for _, res := range results {
		row := map[string]any{"id": res.ID}
		if res.Error != "" {
			row["error"] = res.Error
			out = append(out, row)
			continue
		}
		if res.Entity != nil {
			row["entity"] = entityDTO(res.Entity)
		}
		if res.Statements != nil {
			arr := make([]any, 0, len(res.Statements))
			for i := range res.Statements {
				arr = append(arr, statementDTO(&res.Statements[i]))
			}
			row["statements"] = arr
		}
		out = append(out, row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": out})
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
