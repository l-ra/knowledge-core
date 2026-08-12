package apihttp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	apihttp "github.com/l-ra/knowledge-core/internal/api/http"
	"github.com/l-ra/knowledge-core/internal/auth"
	"github.com/l-ra/knowledge-core/internal/config"
	"github.com/l-ra/knowledge-core/internal/engine"
	"github.com/l-ra/knowledge-core/internal/store"
)

var (
	testAuthEng *auth.Engine
	testStore   *store.Store
)

func setupTestHandler(t *testing.T) http.Handler {
	t.Helper()
	dsn := os.Getenv("KC_DATABASE_URL")
	if dsn == "" {
		t.Skip("KC_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := store.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	mig := filepath.Join("..", "..", "..", "migrations")
	if err := store.Migrate(ctx, pool, mig); err != nil {
		t.Fatal(err)
	}

	_, _ = pool.Exec(ctx, `
		TRUNCATE change_set_item, change_set,
			outbox_event, projection_search, projection_rdf,
			entity_iri_alias,
			release_object, release_dependency, release,
			package_dependency, package,
			lens_definition, validation_report, shape_profile,
			class_profile, property_profile,
			statement_revision_qualifier, statement_revision_reference,
			statement_qualifier, statement_reference, reference,
			statement_revision, entity_revision,
			statement_current, statement,
			entity_label, entity_description,
			entity, auth_runtime, user_changeset_draft RESTART IDENTITY CASCADE;
		UPDATE id_counter SET last_value = 0;
		UPDATE model_schema_config SET instance_of_property = '', model_properties = '[]'::jsonb, updated_at = now() WHERE id = 1;
	`)
	_, _ = pool.Exec(ctx, `DELETE FROM auth_policy WHERE name <> 'bootstrap-admin'`)

	st := store.New(pool)
	testStore = st
	authEng := auth.NewEngine(st, "test-admin")
	if err := authEng.Reload(ctx); err != nil {
		t.Fatal(err)
	}
	testAuthEng = authEng
	cfg := config.Config{AuthMode: "dev", BootstrapAdminSubject: "test-admin"}
	h := apihttp.New(engine.New(st, authEng), st, apihttp.NewAuthenticator(cfg), cfg)

	// Default package required for all writes.
	createPkg(t, h, "test", nil)
	return h
}

func adminHeaders() map[string]string {
	return map[string]string{
		"X-Subject": "test-admin",
		"X-Roles":   "admin",
	}
}

func userHeaders(subject, roles string) map[string]string {
	return map[string]string{
		"X-Subject": subject,
		"X-Roles":   roles,
	}
}

func seedPolicy(t *testing.T, name string, priority int, doc auth.PolicyDocument) {
	t.Helper()
	ctx := context.Background()
	if err := testStore.UpsertAuthPolicy(ctx, name, priority, doc); err != nil {
		t.Fatal(err)
	}
	if err := testAuthEng.Reload(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestAcceptanceSPACallbackFallback(t *testing.T) {
	h := setupTestHandler(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ui/callback?code=x&state=y", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("SPA /ui/callback: %d %s", rec.Code, rec.Body.String())
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Fatalf("expected html, got %q body=%q", ct, rec.Body.String()[:min(80, rec.Body.Len())])
	}
}

func TestAcceptanceListAndUIConfig(t *testing.T) {
	h := setupTestHandler(t)
	_ = createEntity(t, h, "List Me")
	_ = createProperty(t, h, "Listed Prop")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/ui/config", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ui config: %d %s", rec.Code, rec.Body.String())
	}

	ents := doJSON(t, h, http.MethodGet, "/v1/entities?limit=10", nil, nil)
	if ents.StatusCode != http.StatusOK {
		t.Fatalf("list entities: %d %s", ents.StatusCode, ents.Body)
	}
	props := doJSON(t, h, http.MethodGet, "/v1/properties?limit=10", nil, nil)
	if props.StatusCode != http.StatusOK {
		t.Fatalf("list properties: %d %s", props.StatusCode, props.Body)
	}
	lenses := doJSON(t, h, http.MethodGet, "/v1/lenses", nil, nil)
	if lenses.StatusCode != http.StatusOK {
		t.Fatalf("list lenses: %d %s", lenses.StatusCode, lenses.Body)
	}
	pkgs := doJSON(t, h, http.MethodGet, "/v1/packages", nil, nil)
	if pkgs.StatusCode != http.StatusOK {
		t.Fatalf("list packages: %d %s", pkgs.StatusCode, pkgs.Body)
	}
	me := doJSON(t, h, http.MethodGet, "/v1/me", nil, nil)
	if me.StatusCode != http.StatusOK {
		t.Fatalf("me: %d %s", me.StatusCode, me.Body)
	}
}

func TestAcceptanceAdminAuthRuntime(t *testing.T) {
	h := setupTestHandler(t)

	got := doJSON(t, h, http.MethodGet, "/v1/admin/auth", nil, nil)
	if got.StatusCode != http.StatusOK {
		t.Fatalf("get admin auth: %d %s", got.StatusCode, got.Body)
	}
	var wrap struct {
		Instructions struct {
			IdP []string `json:"idp"`
			KC  []string `json:"knowledgeCore"`
		} `json:"instructions"`
		AuthMode string `json:"authMode"`
	}
	if err := json.Unmarshal([]byte(got.Body), &wrap); err != nil {
		t.Fatal(err)
	}
	if len(wrap.Instructions.IdP) == 0 || len(wrap.Instructions.KC) == 0 {
		t.Fatalf("expected setup instructions, got %+v", wrap.Instructions)
	}

	forbidden := doJSON(t, h, http.MethodGet, "/v1/admin/auth", nil, userHeaders("viewer", "reader"))
	if forbidden.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin, got %d %s", forbidden.StatusCode, forbidden.Body)
	}

	// Invalid issuer must fail before persisting.
	bad := doJSON(t, h, http.MethodPut, "/v1/admin/auth", map[string]any{
		"authMode":     "oidc",
		"oidcIssuer":   "http://127.0.0.1:9",
		"oidcClientId": "test-client",
	}, nil)
	if bad.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected bad issuer 400, got %d %s", bad.StatusCode, bad.Body)
	}

	// Switch back to bootstrap without discovery (no issuer required).
	ok := doJSON(t, h, http.MethodPut, "/v1/admin/auth", map[string]any{
		"authMode": "bootstrap",
	}, nil)
	if ok.StatusCode != http.StatusOK {
		t.Fatalf("put bootstrap: %d %s", ok.StatusCode, ok.Body)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/ui/config", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ui config: %d %s", rec.Code, rec.Body.String())
	}
	var cfg map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg["authMode"] != "bootstrap" {
		t.Fatalf("ui config authMode=%v want bootstrap", cfg["authMode"])
	}
}

func TestAcceptancePolicyAPI(t *testing.T) {
	h := setupTestHandler(t)

	qid := createEntity(t, h, "Policy Target")
	create := doJSON(t, h, http.MethodPost, "/v1/policies", map[string]any{
		"name":     "api-viewer",
		"priority": 200,
		"document": map[string]any{
			"effect":     "allow",
			"operations": []string{"discover", "read"},
			"roles":      []string{"viewer"},
			"resource":   map[string]any{"type": "entity", "publicId": qid},
		},
	}, nil)
	if create.StatusCode != http.StatusOK {
		t.Fatalf("create policy: %d %s", create.StatusCode, create.Body)
	}

	get := doJSON(t, h, http.MethodGet, "/v1/policies/api-viewer", nil, nil)
	if get.StatusCode != http.StatusOK {
		t.Fatalf("get policy: %d %s", get.StatusCode, get.Body)
	}

	viewer := userHeaders("U-viewer", "viewer")
	ok := doJSON(t, h, http.MethodGet, "/v1/entities/"+qid, nil, viewer)
	if ok.StatusCode != http.StatusOK {
		t.Fatalf("viewer should discover after policy API upsert: %d %s", ok.StatusCode, ok.Body)
	}

	list := doJSON(t, h, http.MethodGet, "/v1/policies", nil, nil)
	if list.StatusCode != http.StatusOK {
		t.Fatalf("list policies: %d %s", list.StatusCode, list.Body)
	}

	del := doJSON(t, h, http.MethodDelete, "/v1/policies/api-viewer", nil, nil)
	if del.StatusCode != http.StatusOK {
		t.Fatalf("delete policy: %d %s", del.StatusCode, del.Body)
	}
	hidden := doJSON(t, h, http.MethodGet, "/v1/entities/"+qid, nil, viewer)
	if hidden.StatusCode != http.StatusNotFound {
		t.Fatalf("after delete policy entity should 404, got %d", hidden.StatusCode)
	}

	protect := doJSON(t, h, http.MethodDelete, "/v1/policies/bootstrap-admin", nil, nil)
	if protect.StatusCode != http.StatusBadRequest {
		t.Fatalf("bootstrap-admin must be protected, got %d %s", protect.StatusCode, protect.Body)
	}
}

func TestAcceptanceNestedLensAndMany(t *testing.T) {
	h := setupTestHandler(t)

	pCode := createProperty(t, h, "code")
	pName := createProperty(t, h, "name")
	pTag := createProperty(t, h, "tag")
	pOwnerRes := doJSON(t, h, http.MethodPost, "/v1/properties", map[string]any{
		"packageCode": "test",
		"datatype": "EntityReference",
		"labels":   map[string]string{"en": "ownerRef"},
	}, nil)
	if pOwnerRes.StatusCode != http.StatusCreated {
		t.Fatalf("owner property: %d %s", pOwnerRes.StatusCode, pOwnerRes.Body)
	}
	pOwner := parseDataID(t, pOwnerRes.Body)

	owner := createEntity(t, h, "Owner Org")
	_ = createStatement(t, h, owner, pCode, "OWNER-1")
	_ = createStatement(t, h, owner, pName, "Architecture")

	app := createEntity(t, h, "App")
	_ = createStatement(t, h, app, pCode, "APP-1")
	_ = createStatement(t, h, app, pName, "CRM")
	ownerStmt := doJSON(t, h, http.MethodPost, "/v1/statements", map[string]any{
		"packageCode": "test",
		"subject":  app,
		"property": pOwner,
		"value":    map[string]any{"type": "EntityReference", "entityId": owner},
	}, nil)
	if ownerStmt.StatusCode != http.StatusCreated {
		t.Fatalf("owner ref: %d %s", ownerStmt.StatusCode, ownerStmt.Body)
	}

	orgLens := doJSON(t, h, http.MethodPost, "/v1/lenses", map[string]any{
		"code":   "organization",
		"labels": map[string]string{"en": "Organization"},
		"document": map[string]any{
			"key": map[string]any{"property": pCode},
			"fields": map[string]any{
				"name": map[string]any{"property": pName, "type": "String", "cardinality": "one"},
			},
		},
	}, nil)
	if orgLens.StatusCode != http.StatusCreated {
		t.Fatalf("org lens: %d %s", orgLens.StatusCode, orgLens.Body)
	}

	appLens := doJSON(t, h, http.MethodPost, "/v1/lenses", map[string]any{
		"code":   "application",
		"labels": map[string]string{"en": "Application"},
		"document": map[string]any{
			"key": map[string]any{"property": pCode},
			"fields": map[string]any{
				"name": map[string]any{"property": pName, "type": "String", "cardinality": "one"},
				"owner": map[string]any{
					"property": pOwner, "type": "EntityReference", "cardinality": "zeroOrOne",
					"lens": "organization",
				},
				"tags": map[string]any{"property": pTag, "type": "String", "cardinality": "many"},
			},
		},
	}, nil)
	if appLens.StatusCode != http.StatusCreated {
		t.Fatalf("app lens: %d %s", appLens.StatusCode, appLens.Body)
	}

	read := doJSON(t, h, http.MethodGet, "/v1/lenses/application/instances/APP-1", nil, nil)
	if read.StatusCode != http.StatusOK {
		t.Fatalf("nested read: %d %s", read.StatusCode, read.Body)
	}
	var view map[string]any
	_ = json.Unmarshal([]byte(read.Body), &view)
	ownerView, ok := view["owner"].(map[string]any)
	if !ok || ownerView["name"] != "Architecture" {
		t.Fatalf("expected nested owner name, got %v", view["owner"])
	}

	add := doJSON(t, h, http.MethodPost, "/v1/lenses/application/instances/APP-1/patch", map[string]any{
		"operations": []map[string]any{
			{"op": "add", "field": "tags", "value": map[string]any{"type": "String", "string": "alpha"}},
			{"op": "add", "field": "tags", "value": map[string]any{"type": "String", "string": "beta"}},
		},
	}, nil)
	if add.StatusCode != http.StatusOK {
		t.Fatalf("add tags: %d %s", add.StatusCode, add.Body)
	}
	var afterAdd struct {
		Data map[string]any `json:"data"`
	}
	_ = json.Unmarshal([]byte(add.Body), &afterAdd)
	tags, _ := afterAdd.Data["tags"].([]any)
	if len(tags) != 2 {
		t.Fatalf("expected 2 tags, got %v", afterAdd.Data["tags"])
	}

	rm := doJSON(t, h, http.MethodPost, "/v1/lenses/application/instances/APP-1/patch", map[string]any{
		"operations": []map[string]any{
			{"op": "remove", "field": "tags", "value": map[string]any{"type": "String", "string": "alpha"}},
		},
	}, nil)
	if rm.StatusCode != http.StatusOK {
		t.Fatalf("remove tag: %d %s", rm.StatusCode, rm.Body)
	}
	var afterRm struct {
		Data map[string]any `json:"data"`
	}
	_ = json.Unmarshal([]byte(rm.Body), &afterRm)
	tags2, _ := afterRm.Data["tags"].([]any)
	if len(tags2) != 1 || tags2[0] != "beta" {
		t.Fatalf("expected only beta, got %v", afterRm.Data["tags"])
	}
}

func TestAcceptanceA1A2A14(t *testing.T) {
	h := setupTestHandler(t)

	res := doJSON(t, h, http.MethodPost, "/v1/entities", map[string]any{
		"packageCode": "test",
		"labels": map[string]string{"cs": "Zákazník"},
	}, nil)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("A2: want 400, got %d %s", res.StatusCode, res.Body)
	}

	ent := doJSON(t, h, http.MethodPost, "/v1/entities", map[string]any{
		"packageCode": "test",
		"labels": map[string]string{"en": "Customer"},
	}, nil)
	if ent.StatusCode != http.StatusCreated {
		t.Fatalf("create entity: %d %s", ent.StatusCode, ent.Body)
	}
	qid := parseDataID(t, ent.Body)

	prop := doJSON(t, h, http.MethodPost, "/v1/properties", map[string]any{
		"packageCode": "test",
		"datatype": "String",
		"labels":   map[string]string{"en": "Code"},
	}, nil)
	if prop.StatusCode != http.StatusCreated {
		t.Fatalf("create property: %d %s", prop.StatusCode, prop.Body)
	}
	pid := parseDataID(t, prop.Body)

	stmt := doJSON(t, h, http.MethodPost, "/v1/statements", map[string]any{
		"packageCode": "test",
		"subject":  qid,
		"property": pid,
		"value":    map[string]any{"type": "String", "string": "CRM-01"},
	}, nil)
	if stmt.StatusCode != http.StatusCreated {
		t.Fatalf("create statement: %d %s", stmt.StatusCode, stmt.Body)
	}
	sid := parseDataID(t, stmt.Body)

	got := doJSON(t, h, http.MethodGet, "/v1/statements/"+sid, nil, nil)
	if got.StatusCode != http.StatusOK {
		t.Fatalf("get statement: %d %s", got.StatusCode, got.Body)
	}
	list := doJSON(t, h, http.MethodGet, "/v1/entities/"+qid+"/statements", nil, nil)
	if list.StatusCode != http.StatusOK {
		t.Fatalf("list: %d %s", list.StatusCode, list.Body)
	}
	var listBody struct {
		Statements []map[string]any `json:"statements"`
	}
	_ = json.Unmarshal([]byte(list.Body), &listBody)
	if len(listBody.Statements) != 1 {
		t.Fatalf("A14: expected 1 statement in current, got %d", len(listBody.Statements))
	}
}

func TestAcceptanceA7A8A9(t *testing.T) {
	h := setupTestHandler(t)

	qid := createEntity(t, h, "App")
	pid := createProperty(t, h, "Code")
	s1 := createStatement(t, h, qid, pid, "v1")
	s2 := createStatement(t, h, qid, pid, "v2")
	s3 := createStatement(t, h, qid, pid, "v3")

	// A8 — stale revision conflict
	conflict := doJSON(t, h, http.MethodPost, "/v1/statements/"+s1+"/revise", map[string]any{
		"expectedRevision": 1,
		"value":            map[string]any{"type": "String", "string": "A-first"},
	}, nil)
	if conflict.StatusCode != http.StatusOK {
		t.Fatalf("first revise: %d %s", conflict.StatusCode, conflict.Body)
	}
	stale := doJSON(t, h, http.MethodPost, "/v1/statements/"+s1+"/revise", map[string]any{
		"expectedRevision": 1,
		"value":            map[string]any{"type": "String", "string": "A-stale"},
	}, nil)
	if stale.StatusCode != http.StatusConflict {
		t.Fatalf("A8: want 409, got %d %s", stale.StatusCode, stale.Body)
	}

	// A7 — atomic batch: third op has wrong revision → none committed
	batchBody := map[string]any{
		"operationType": "BatchRevise",
		"operations": []map[string]any{
			{"op": "reviseStatement", "statement": s2, "expectedRevision": 1, "value": map[string]any{"type": "String", "string": "B-new"}},
			{"op": "reviseStatement", "statement": s3, "expectedRevision": 1, "value": map[string]any{"type": "String", "string": "C-new"}},
			{"op": "reviseStatement", "statement": s2, "expectedRevision": 1, "value": map[string]any{"type": "String", "string": "B-conflict"}},
		},
	}
	batch := doJSON(t, h, http.MethodPost, "/v1/changesets", batchBody, nil)
	if batch.StatusCode != http.StatusConflict {
		t.Fatalf("A7: want 409, got %d %s", batch.StatusCode, batch.Body)
	}
	s2got := doJSON(t, h, http.MethodGet, "/v1/statements/"+s2, nil, nil)
	s3got := doJSON(t, h, http.MethodGet, "/v1/statements/"+s3, nil, nil)
	if parseStatementStringValue(t, s2got.Body) != "v2" {
		t.Fatalf("A7: S2 should remain v2, got %s", s2got.Body)
	}
	if parseStatementStringValue(t, s3got.Body) != "v3" {
		t.Fatalf("A7: S3 should remain v3, got %s", s3got.Body)
	}

	// A9 — idempotent retry
	idemKey := "test-idem-a9"
	body := map[string]any{
		"expectedRevision": 2,
		"value":            map[string]any{"type": "String", "string": "A-idem"},
	}
	first := doJSON(t, h, http.MethodPost, "/v1/statements/"+s1+"/revise", body, map[string]string{"Idempotency-Key": idemKey})
	second := doJSON(t, h, http.MethodPost, "/v1/statements/"+s1+"/revise", body, map[string]string{"Idempotency-Key": idemKey})
	if first.StatusCode != http.StatusOK || second.StatusCode != http.StatusOK {
		t.Fatalf("A9: idempotent calls failed: %d %s / %d %s", first.StatusCode, first.Body, second.StatusCode, second.Body)
	}
	var cs1, cs2 struct {
		ChangeSet struct {
			ID string `json:"id"`
		} `json:"changeSet"`
	}
	_ = json.Unmarshal([]byte(first.Body), &cs1)
	_ = json.Unmarshal([]byte(second.Body), &cs2)
	if cs1.ChangeSet.ID == "" || cs1.ChangeSet.ID != cs2.ChangeSet.ID {
		t.Fatalf("A9: expected same changeSet id, got %q and %q", cs1.ChangeSet.ID, cs2.ChangeSet.ID)
	}
}

func TestAcceptanceA10(t *testing.T) {
	h := setupTestHandler(t)

	qid := createEntity(t, h, "App")
	ownerProp := createProperty(t, h, "Owner")
	roleProp := createProperty(t, h, "Valid Role")
	_ = createProperty(t, h, "Code")

	// Reference as provenance evidence
	ref := doJSON(t, h, http.MethodPost, "/v1/references", map[string]any{
		"fields": map[string]any{
			"sourceUrl":   "https://example.com/doc.pdf",
			"documentId":  "DOC-123",
			"retrievedAt": "2026-08-10T08:00:00Z",
		},
	}, nil)
	if ref.StatusCode != http.StatusCreated {
		t.Fatalf("create reference: %d %s", ref.StatusCode, ref.Body)
	}
	rid := parseDataID(t, ref.Body)

	stmt := doJSON(t, h, http.MethodPost, "/v1/statements", map[string]any{
		"packageCode": "test",
		"subject":  qid,
		"property": ownerProp,
		"value":    map[string]any{"type": "String", "string": "owner-1"},
		"referenceIds": []string{rid},
		"qualifiers": []map[string]any{
			{"property": roleProp, "value": map[string]any{"type": "String", "string": "business-owner"}},
		},
	}, map[string]string{"X-Actor": "editor-a"})
	if stmt.StatusCode != http.StatusCreated {
		t.Fatalf("create statement: %d %s", stmt.StatusCode, stmt.Body)
	}
	sid := parseDataID(t, stmt.Body)

	// Editor changes qualifier only — reference must stay, new ChangeSet with actor
	rev := doJSON(t, h, http.MethodPost, "/v1/statements/"+sid+"/revise", map[string]any{
		"expectedRevision": 1,
		"qualifiers": []map[string]any{
			{"property": roleProp, "value": map[string]any{"type": "String", "string": "technical-owner"}},
		},
	}, map[string]string{"X-Actor": "editor-b"})
	if rev.StatusCode != http.StatusOK {
		t.Fatalf("revise qualifier: %d %s", rev.StatusCode, rev.Body)
	}

	var revBody struct {
		Data struct {
			ReferenceIDs []string `json:"referenceIds"`
			Qualifiers   []struct {
				Value map[string]any `json:"value"`
			} `json:"qualifiers"`
		} `json:"data"`
		ChangeSet struct {
			ID    string `json:"id"`
			Actor string `json:"actor"`
			Items []struct {
				Op string `json:"op"`
			} `json:"items"`
		} `json:"changeSet"`
	}
	_ = json.Unmarshal([]byte(rev.Body), &revBody)
	if len(revBody.Data.ReferenceIDs) != 1 || revBody.Data.ReferenceIDs[0] != rid {
		t.Fatalf("A10: reference should remain %s, got %v", rid, revBody.Data.ReferenceIDs)
	}
	if revBody.Data.Qualifiers[0].Value["string"] != "technical-owner" {
		t.Fatalf("A10: qualifier not updated")
	}
	if revBody.ChangeSet.ID == "" || revBody.ChangeSet.Actor != "editor-b" {
		t.Fatalf("A10: expected changeset audit from editor-b, got %+v", revBody.ChangeSet)
	}

	// Reference object itself unchanged
	gotRef := doJSON(t, h, http.MethodGet, "/v1/references/"+rid, nil, nil)
	var refBody struct {
		Fields map[string]any `json:"fields"`
	}
	_ = json.Unmarshal([]byte(gotRef.Body), &refBody)
	if refBody.Fields["documentId"] != "DOC-123" {
		t.Fatalf("A10: reference provenance mutated")
	}

	// History shows qualifier change between revisions, reference on both
	hist := doJSON(t, h, http.MethodGet, "/v1/statements/"+sid+"/history", nil, nil)
	var histBody struct {
		Revisions []struct {
			RevisionNo   int      `json:"revisionNo"`
			ReferenceIDs []string `json:"referenceIds"`
			Qualifiers   []struct {
				Value map[string]any `json:"value"`
			} `json:"qualifiers"`
		} `json:"revisions"`
	}
	_ = json.Unmarshal([]byte(hist.Body), &histBody)
	if len(histBody.Revisions) != 2 {
		t.Fatalf("A10: want 2 revisions, got %d", len(histBody.Revisions))
	}
	if len(histBody.Revisions[0].ReferenceIDs) != 1 || len(histBody.Revisions[1].ReferenceIDs) != 1 {
		t.Fatalf("A10: reference missing in history")
	}
	if histBody.Revisions[0].Qualifiers[0].Value["string"] != "business-owner" {
		t.Fatalf("A10: rev1 qualifier wrong")
	}
	if histBody.Revisions[1].Qualifiers[0].Value["string"] != "technical-owner" {
		t.Fatalf("A10: rev2 qualifier wrong")
	}
}

func TestAcceptanceA11A12A13(t *testing.T) {
	h := setupTestHandler(t)

	createPkg(t, h, "package-a", nil)
	createPkg(t, h, "package-b", nil)
	createPkg(t, h, "experimental-foo", nil)
	createPkg(t, h, "architecture-model", nil)

	// Package A owns entity; package B owns cross-ref statement
	qid := createEntityWithPkg(t, h, "package-a", "Entity A")
	pid := createPropertyWithPkg(t, h, "package-a", "Code")
	_ = createStatementWithPkg(t, h, "package-b", qid, pid, "cross")

	// Experimental data not part of architecture-model release
	_ = createEntityWithPkg(t, h, "experimental-foo", "Experiment")

	pubA := doJSON(t, h, http.MethodPost, "/v1/packages/package-a/releases", map[string]any{"version": "1.0.0"}, nil)
	if pubA.StatusCode != http.StatusCreated {
		t.Fatalf("publish A: %d %s", pubA.StatusCode, pubA.Body)
	}
	var relA struct {
		Data struct {
			Objects []struct {
				ObjectType string `json:"objectType"`
				PublicID   string `json:"publicId"`
			} `json:"objects"`
		} `json:"data"`
	}
	_ = json.Unmarshal([]byte(pubA.Body), &relA)
	for _, o := range relA.Data.Objects {
		if o.ObjectType == "statement" {
			t.Fatalf("A11: package-a release must not include statements from package-b")
		}
	}

	pubArch := doJSON(t, h, http.MethodPost, "/v1/packages/architecture-model/releases", map[string]any{"version": "3.4.0"}, nil)
	if pubArch.StatusCode != http.StatusCreated {
		t.Fatalf("publish arch: %d %s", pubArch.StatusCode, pubArch.Body)
	}
	bundle := doJSON(t, h, http.MethodGet, "/v1/packages/architecture-model/releases/3.4.0/bundle", nil, nil)
	var b struct {
		Manifest struct {
			Package string `json:"package"`
		} `json:"manifest"`
		Entities []any `json:"entities"`
	}
	_ = json.Unmarshal([]byte(bundle.Body), &b)
	if b.Manifest.Package != "architecture-model" {
		t.Fatalf("A12: wrong package in bundle")
	}
	if len(b.Entities) != 0 {
		t.Fatalf("A12: architecture-model release should not include experimental entities, got %d", len(b.Entities))
	}

	dup := doJSON(t, h, http.MethodPost, "/v1/packages/architecture-model/releases", map[string]any{"version": "3.4.0"}, nil)
	if dup.StatusCode != http.StatusConflict {
		t.Fatalf("A13: want 409 on duplicate publish, got %d %s", dup.StatusCode, dup.Body)
	}
	mut := doJSON(t, h, http.MethodPost, "/v1/packages/architecture-model/releases/3.4.0/mutate", nil, nil)
	if mut.StatusCode != http.StatusConflict {
		t.Fatalf("A13: want 409 on mutate, got %d %s", mut.StatusCode, mut.Body)
	}
}

func TestAcceptanceImportPromotion(t *testing.T) {
	h := setupTestHandler(t)

	createPkg(t, h, "architecture-model", nil)
	createPkg(t, h, "experimental-foo", nil)

	qid := createEntityWithPkg(t, h, "architecture-model", "Arch Entity")
	pid := createPropertyWithPkg(t, h, "architecture-model", "Code")
	sid := createStatementWithPkg(t, h, "architecture-model", qid, pid, "promoted")
	_ = createEntityWithPkg(t, h, "experimental-foo", "Experiment")

	pub := doJSON(t, h, http.MethodPost, "/v1/packages/architecture-model/releases", map[string]any{"version": "3.4.0"}, nil)
	if pub.StatusCode != http.StatusCreated {
		t.Fatalf("publish: %d %s", pub.StatusCode, pub.Body)
	}

	bundleRes := doJSON(t, h, http.MethodGet, "/v1/packages/architecture-model/releases/3.4.0/bundle", nil, nil)
	if bundleRes.StatusCode != http.StatusOK {
		t.Fatalf("export bundle: %d %s", bundleRes.StatusCode, bundleRes.Body)
	}

	truncateTestDB(t)

	var bundle any
	if err := json.Unmarshal([]byte(bundleRes.Body), &bundle); err != nil {
		t.Fatal(err)
	}
	imp := doJSON(t, h, http.MethodPost, "/v1/releases/import", bundle, nil)
	if imp.StatusCode != http.StatusCreated {
		t.Fatalf("import: %d %s", imp.StatusCode, imp.Body)
	}

	rel := doJSON(t, h, http.MethodGet, "/v1/packages/architecture-model/releases/3.4.0", nil, nil)
	if rel.StatusCode != http.StatusOK {
		t.Fatalf("get release after import: %d %s", rel.StatusCode, rel.Body)
	}

	ent := doJSON(t, h, http.MethodGet, "/v1/entities/"+qid, nil, nil)
	if ent.StatusCode != http.StatusOK {
		t.Fatalf("imported entity missing: %d %s", ent.StatusCode, ent.Body)
	}

	st := doJSON(t, h, http.MethodGet, "/v1/statements/"+sid, nil, nil)
	if st.StatusCode != http.StatusOK {
		t.Fatalf("imported statement missing: %d %s", st.StatusCode, st.Body)
	}

	expPkg := doJSON(t, h, http.MethodGet, "/v1/packages/experimental-foo", nil, nil)
	if expPkg.StatusCode != http.StatusNotFound {
		t.Fatalf("experimental package should not be promoted, got %d", expPkg.StatusCode)
	}

	dup := doJSON(t, h, http.MethodPost, "/v1/releases/import", bundle, nil)
	if dup.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate import: want 409, got %d %s", dup.StatusCode, dup.Body)
	}
}

func TestAcceptanceA4A5A6(t *testing.T) {
	h := setupTestHandler(t)
	admin := adminHeaders()

	qid := createEntity(t, h, "Secured App")
	pName := createProperty(t, h, "name")
	pOwner := createProperty(t, h, "owner")
	pSecret := createProperty(t, h, "secretClassification")

	sName := createStatement(t, h, qid, pName, "Alpha")
	sOwner := createStatement(t, h, qid, pOwner, "Team A")
	sSecret := createStatement(t, h, qid, pSecret, "TOP SECRET")

	seedPolicy(t, "limited-editor", 500, auth.PolicyDocument{
		Effect:     "allow",
		Operations: []auth.Operation{auth.OpDiscover, auth.OpRead, auth.OpUpdate},
		Roles:      []string{"limited-editor"},
		Resource:   auth.PolicyResource{Type: auth.ResourceEntity, PublicID: qid},
		Properties: []string{pName, pOwner},
	})
	seedPolicy(t, "viewer-q1", 500, auth.PolicyDocument{
		Effect:     "allow",
		Operations: []auth.Operation{auth.OpDiscover, auth.OpRead},
		Roles:      []string{"viewer"},
		Resource:   auth.PolicyResource{Type: auth.ResourceEntity, PublicID: qid},
		Properties: []string{pName},
	})

	limited := userHeaders("U-limited", "limited-editor")

	rev := doJSON(t, h, http.MethodPost, "/v1/statements/"+sName+"/revise", map[string]any{
		"expectedRevision": 1,
		"value":            map[string]any{"type": "String", "string": "Beta"},
	}, limited)
	if rev.StatusCode != http.StatusOK {
		t.Fatalf("A4: revise name: %d %s", rev.StatusCode, rev.Body)
	}

	secretAfter := doJSON(t, h, http.MethodGet, "/v1/statements/"+sSecret, nil, admin)
	if secretAfter.StatusCode != http.StatusOK {
		t.Fatalf("A4: admin read secret: %d %s", secretAfter.StatusCode, secretAfter.Body)
	}
	if parseStatementStringValue(t, secretAfter.Body) != "TOP SECRET" {
		t.Fatalf("A4: secretClassification changed")
	}

	list := doJSON(t, h, http.MethodGet, "/v1/entities/"+qid+"/statements", nil, userHeaders("U-viewer", "viewer"))
	var listBody struct {
		Statements []struct {
			Property string `json:"property"`
		} `json:"statements"`
	}
	_ = json.Unmarshal([]byte(list.Body), &listBody)
	if list.StatusCode != http.StatusOK {
		t.Fatalf("A5: list statements: %d %s", list.StatusCode, list.Body)
	}
	if len(listBody.Statements) != 1 || listBody.Statements[0].Property != pName {
		t.Fatalf("A5: expected only name statement visible, got %+v", listBody.Statements)
	}
	secretRead := doJSON(t, h, http.MethodGet, "/v1/statements/"+sSecret, nil, userHeaders("U-viewer", "viewer"))
	if secretRead.StatusCode != http.StatusNotFound {
		t.Fatalf("A5: secret statement should 404, got %d", secretRead.StatusCode)
	}

	qHidden := createEntity(t, h, "Hidden Entity")
	hidden := doJSON(t, h, http.MethodGet, "/v1/entities/"+qHidden, nil, userHeaders("U-viewer", "viewer"))
	if hidden.StatusCode != http.StatusNotFound {
		t.Fatalf("A6: hidden entity should 404, got %d %s", hidden.StatusCode, hidden.Body)
	}

	_ = sOwner
}

func TestAcceptanceA3(t *testing.T) {
	h := setupTestHandler(t)

	pCode := createProperty(t, h, "code")
	pName := createProperty(t, h, "name")
	pOwner := createProperty(t, h, "owner")

	qid := createEntity(t, h, "Application")
	_ = createStatement(t, h, qid, pCode, "CRM")
	_ = createStatement(t, h, qid, pName, "CRM Platform")
	_ = createStatement(t, h, qid, pOwner, "Architecture Team")

	lens := doJSON(t, h, http.MethodPost, "/v1/lenses", map[string]any{
		"code":   "application",
		"labels": map[string]string{"en": "Application"},
		"document": map[string]any{
			"key": map[string]any{"property": pCode},
			"fields": map[string]any{
				"name":  map[string]any{"property": pName, "type": "String", "cardinality": "one"},
				"owner": map[string]any{"property": pOwner, "type": "String", "cardinality": "zeroOrOne"},
			},
		},
	}, nil)
	if lens.StatusCode != http.StatusCreated {
		t.Fatalf("create lens: %d %s", lens.StatusCode, lens.Body)
	}

	read := doJSON(t, h, http.MethodGet, "/v1/lenses/application/instances/CRM", nil, nil)
	if read.StatusCode != http.StatusOK {
		t.Fatalf("A3 read: %d %s", read.StatusCode, read.Body)
	}
	var view map[string]any
	if err := json.Unmarshal([]byte(read.Body), &view); err != nil {
		t.Fatal(err)
	}
	if view["name"] != "CRM Platform" || view["owner"] != "Architecture Team" {
		t.Fatalf("A3: unexpected view %v", view)
	}
	for _, k := range []string{"Q", "P", "S"} {
		body := read.Body
		if strings.Contains(body, `"`+k) && regexp.MustCompile(`"`+k+`\d+"`).MatchString(body) {
			t.Fatalf("A3: response must not expose %s IDs: %s", k, body)
		}
	}

	gql := doJSON(t, h, http.MethodPost, "/v1/graphql", map[string]any{
		"query": `query { application(code: "CRM") { name owner } }`,
	}, nil)
	if gql.StatusCode != http.StatusOK {
		t.Fatalf("A3 graphql: %d %s", gql.StatusCode, gql.Body)
	}
	var gqlOut struct {
		Data struct {
			Application map[string]any `json:"application"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(gql.Body), &gqlOut); err != nil {
		t.Fatal(err)
	}
	if gqlOut.Data.Application["name"] != "CRM Platform" {
		t.Fatalf("A3 graphql name: %v", gqlOut.Data.Application)
	}
}

func TestAcceptanceA15(t *testing.T) {
	h := setupTestHandler(t)

	qid := createEntity(t, h, "Searchable Customer")
	pid := createProperty(t, h, "Code")
	_ = createStatement(t, h, qid, pid, "UNIQUE-TOKEN-42")

	proc := doJSON(t, h, http.MethodPost, "/v1/projections/outbox/process", nil, nil)
	if proc.StatusCode != http.StatusOK {
		t.Fatalf("process outbox: %d %s", proc.StatusCode, proc.Body)
	}

	search1 := doJSON(t, h, http.MethodGet, "/v1/projections/search?q=UNIQUE-TOKEN-42", nil, nil)
	if search1.StatusCode != http.StatusOK {
		t.Fatalf("search1: %d %s", search1.StatusCode, search1.Body)
	}
	var res1 struct {
		Results []struct {
			PublicID string `json:"publicId"`
		} `json:"results"`
	}
	_ = json.Unmarshal([]byte(search1.Body), &res1)
	if len(res1.Results) == 0 {
		t.Fatalf("A15: expected search hit before rebuild")
	}

	ctx := context.Background()
	if _, err := testStore.Pool().Exec(ctx, `TRUNCATE projection_search`); err != nil {
		t.Fatal(err)
	}

	empty := doJSON(t, h, http.MethodGet, "/v1/projections/search?q=UNIQUE-TOKEN-42", nil, nil)
	var resEmpty struct {
		Results []any `json:"results"`
	}
	_ = json.Unmarshal([]byte(empty.Body), &resEmpty)
	if len(resEmpty.Results) != 0 {
		t.Fatalf("A15: projection should be empty after truncate")
	}

	rebuild := doJSON(t, h, http.MethodPost, "/v1/projections/search/rebuild", nil, nil)
	if rebuild.StatusCode != http.StatusOK {
		t.Fatalf("rebuild: %d %s", rebuild.StatusCode, rebuild.Body)
	}

	search2 := doJSON(t, h, http.MethodGet, "/v1/projections/search?q=UNIQUE-TOKEN-42", nil, nil)
	if search2.StatusCode != http.StatusOK {
		t.Fatalf("search2: %d %s", search2.StatusCode, search2.Body)
	}
	var res2 struct {
		Results []struct {
			ObjectType string `json:"objectType"`
			PublicID   string `json:"publicId"`
		} `json:"results"`
	}
	_ = json.Unmarshal([]byte(search2.Body), &res2)
	if len(res2.Results) == 0 {
		t.Fatalf("A15: expected search hit after rebuild from canonical")
	}
	foundStatement := false
	for _, r := range res2.Results {
		if r.ObjectType == "statement" {
			foundStatement = true
		}
	}
	if !foundStatement {
		t.Fatalf("A15: rebuild should include statement projection")
	}
}

func TestAcceptanceSearchACL(t *testing.T) {
	h := setupTestHandler(t)

	qVisible := createEntity(t, h, "Visible Search Target")
	qHidden := createEntity(t, h, "Hidden Search Target")
	pName := createProperty(t, h, "name")
	pSecret := createProperty(t, h, "secretToken")

	_ = createStatement(t, h, qVisible, pName, "ACL-TOKEN-VISIBLE")
	sSecret := createStatement(t, h, qVisible, pSecret, "ACL-TOKEN-SECRET")
	_ = createStatement(t, h, qHidden, pName, "ACL-TOKEN-HIDDEN")

	seedPolicy(t, "search-viewer", 500, auth.PolicyDocument{
		Effect:     "allow",
		Operations: []auth.Operation{auth.OpDiscover, auth.OpRead},
		Roles:      []string{"viewer"},
		Resource:   auth.PolicyResource{Type: auth.ResourceEntity, PublicID: qVisible},
		Properties: []string{pName},
	})

	proc := doJSON(t, h, http.MethodPost, "/v1/projections/outbox/process", nil, nil)
	if proc.StatusCode != http.StatusOK {
		t.Fatalf("process outbox: %d %s", proc.StatusCode, proc.Body)
	}

	viewer := userHeaders("U-viewer", "viewer")

	visible := doJSON(t, h, http.MethodGet, "/v1/projections/search?q=ACL-TOKEN-VISIBLE", nil, viewer)
	if visible.StatusCode != http.StatusOK {
		t.Fatalf("search visible: %d %s", visible.StatusCode, visible.Body)
	}
	var visRes struct {
		Results []struct {
			ObjectType  string `json:"objectType"`
			PublicID    string `json:"publicId"`
			PropertyPID string `json:"propertyPid"`
		} `json:"results"`
	}
	_ = json.Unmarshal([]byte(visible.Body), &visRes)
	if len(visRes.Results) == 0 {
		t.Fatalf("viewer should see allowed statement hits")
	}
	for _, r := range visRes.Results {
		if r.ObjectType == "statement" && r.PropertyPID == pSecret {
			t.Fatalf("viewer must not see secret property hit")
		}
		if r.PublicID == qHidden || r.PublicID == sSecret {
			t.Fatalf("viewer must not see hidden/secret publicId %s", r.PublicID)
		}
	}

	hidden := doJSON(t, h, http.MethodGet, "/v1/projections/search?q=ACL-TOKEN-HIDDEN", nil, viewer)
	var hidRes struct {
		Results []any `json:"results"`
	}
	_ = json.Unmarshal([]byte(hidden.Body), &hidRes)
	if len(hidRes.Results) != 0 {
		t.Fatalf("viewer must not discover hidden entity via search, got %v", hidRes.Results)
	}

	secret := doJSON(t, h, http.MethodGet, "/v1/projections/search?q=ACL-TOKEN-SECRET", nil, viewer)
	var secRes struct {
		Results []any `json:"results"`
	}
	_ = json.Unmarshal([]byte(secret.Body), &secRes)
	if len(secRes.Results) != 0 {
		t.Fatalf("viewer must not see secret token via search")
	}
}

func TestAcceptanceRDFProjection(t *testing.T) {
	h := setupTestHandler(t)

	qid := createEntity(t, h, "RDF Customer")
	pid := createProperty(t, h, "Code")
	_ = createStatement(t, h, qid, pid, "RDF-TOKEN-1")

	proc := doJSON(t, h, http.MethodPost, "/v1/projections/outbox/process", nil, nil)
	if proc.StatusCode != http.StatusOK {
		t.Fatalf("process outbox: %d %s", proc.StatusCode, proc.Body)
	}

	export1 := doJSON(t, h, http.MethodGet, "/v1/projections/rdf", nil, nil)
	if export1.StatusCode != http.StatusOK {
		t.Fatalf("rdf export: %d %s", export1.StatusCode, export1.Body)
	}
	if !strings.Contains(export1.Body, "RDF Customer") && !strings.Contains(export1.Body, qid) {
		t.Fatalf("rdf export missing entity data: %s", export1.Body)
	}
	if !strings.Contains(export1.Body, "RDF-TOKEN-1") {
		t.Fatalf("rdf export missing statement value: %s", export1.Body)
	}

	ctx := context.Background()
	if _, err := testStore.Pool().Exec(ctx, `TRUNCATE projection_rdf`); err != nil {
		t.Fatal(err)
	}
	empty := doJSON(t, h, http.MethodGet, "/v1/projections/rdf", nil, nil)
	if strings.TrimSpace(empty.Body) != "" {
		t.Fatalf("expected empty rdf after truncate, got %q", empty.Body)
	}

	rebuild := doJSON(t, h, http.MethodPost, "/v1/projections/rdf/rebuild", nil, nil)
	if rebuild.StatusCode != http.StatusOK {
		t.Fatalf("rdf rebuild: %d %s", rebuild.StatusCode, rebuild.Body)
	}
	export2 := doJSON(t, h, http.MethodGet, "/v1/projections/rdf", nil, nil)
	if !strings.Contains(export2.Body, "RDF-TOKEN-1") {
		t.Fatalf("rdf rebuild missing statement value: %s", export2.Body)
	}
}

func TestAcceptanceIRIMapping(t *testing.T) {
	h := setupTestHandler(t)

	createPkg(t, h, "onto", nil)
	patch := doJSON(t, h, http.MethodPatch, "/v1/packages/onto", map[string]any{
		"iriBase": "https://example.org/id",
	}, nil)
	if patch.StatusCode != http.StatusOK {
		t.Fatalf("patch package: %d %s", patch.StatusCode, patch.Body)
	}
	var pkgWrap struct {
		Data struct {
			IRIBase string `json:"iriBase"`
		} `json:"data"`
	}
	_ = json.Unmarshal([]byte(patch.Body), &pkgWrap)
	if pkgWrap.Data.IRIBase != "https://example.org/id/" {
		t.Fatalf("expected normalized iriBase, got %q", pkgWrap.Data.IRIBase)
	}

	entRes := doJSON(t, h, http.MethodPost, "/v1/entities", map[string]any{
		"packageCode": "onto",
		"labels":      map[string]string{"en": "Alice"},
		"iriLocal":    "person/alice",
	}, nil)
	if entRes.StatusCode != http.StatusCreated {
		t.Fatalf("create entity: %d %s", entRes.StatusCode, entRes.Body)
	}
	qid := parseDataID(t, entRes.Body)

	get := doJSON(t, h, http.MethodGet, "/v1/entities/"+qid, nil, nil)
	if get.StatusCode != http.StatusOK {
		t.Fatalf("get entity: %d %s", get.StatusCode, get.Body)
	}
	var ent struct {
		IRI      string `json:"iri"`
		IRILocal string `json:"iriLocal"`
	}
	_ = json.Unmarshal([]byte(get.Body), &ent)
	wantIRI := "https://example.org/id/person/alice"
	if ent.IRI != wantIRI || ent.IRILocal != "person/alice" {
		t.Fatalf("entity iri=%q local=%q want %q", ent.IRI, ent.IRILocal, wantIRI)
	}

	alias := doJSON(t, h, http.MethodPut, "/v1/entities/"+qid+"/iri-aliases", map[string]any{
		"aliases": []map[string]string{{"iri": "https://www.wikidata.org/entity/Q42", "kind": "sameAs"}},
	}, nil)
	if alias.StatusCode != http.StatusOK {
		t.Fatalf("put aliases: %d %s", alias.StatusCode, alias.Body)
	}

	_ = doJSON(t, h, http.MethodPost, "/v1/projections/outbox/process", nil, nil)
	export := doJSON(t, h, http.MethodGet, "/v1/projections/rdf", nil, nil)
	if export.StatusCode != http.StatusOK {
		t.Fatalf("rdf export: %d %s", export.StatusCode, export.Body)
	}
	if !strings.Contains(export.Body, wantIRI) {
		t.Fatalf("rdf missing canonical iri %q: %s", wantIRI, export.Body)
	}
	if !strings.Contains(export.Body, "https://www.wikidata.org/entity/Q42") {
		t.Fatalf("rdf missing sameAs: %s", export.Body)
	}
	if !strings.Contains(export.Body, "owl#sameAs") && !strings.Contains(export.Body, "2002/07/owl#sameAs") {
		t.Fatalf("rdf missing owl:sameAs predicate: %s", export.Body)
	}
}

func TestAcceptanceRDFImport(t *testing.T) {
	h := setupTestHandler(t)

	nt := `
<https://example.org/id/person/alice> <http://www.w3.org/1999/02/22-rdf-syntax-ns#type> <https://example.org/id/class/Person> .
<https://example.org/id/person/alice> <http://www.w3.org/2000/01/rdf-schema#label> "Alice"@en .
<https://example.org/id/class/Person> <http://www.w3.org/1999/02/22-rdf-syntax-ns#type> <http://www.w3.org/2000/01/rdf-schema#Class> .
<https://example.org/id/class/Person> <http://www.w3.org/2000/01/rdf-schema#label> "Person"@en .
<https://example.org/id/prop/age> <http://www.w3.org/1999/02/22-rdf-syntax-ns#type> <http://www.w3.org/1999/02/22-rdf-syntax-ns#Property> .
<https://example.org/id/prop/age> <http://www.w3.org/2000/01/rdf-schema#label> "age"@en .
<https://example.org/id/prop/age> <http://www.w3.org/2000/01/rdf-schema#range> <http://www.w3.org/2001/XMLSchema#integer> .
<https://example.org/id/person/alice> <https://example.org/id/prop/age> "42"^^<http://www.w3.org/2001/XMLSchema#integer> .
`
	prefixes := doJSON(t, h, http.MethodPost, "/v1/rdf/prefixes", map[string]any{
		"turtlePrefixes": "@prefix ex: <https://example.org/id/> .\n@prefix person: <https://example.org/id/person/> .",
	}, nil)
	if prefixes.StatusCode != http.StatusOK {
		t.Fatalf("prefixes: %d %s", prefixes.StatusCode, prefixes.Body)
	}
	if !strings.Contains(prefixes.Body, "https://example.org/id/") || !strings.Contains(prefixes.Body, `"prefix":"ex"`) {
		t.Fatalf("prefixes body: %s", prefixes.Body)
	}

	analyze := doJSON(t, h, http.MethodPost, "/v1/rdf/analyze", map[string]any{
		"ntriples": nt,
		"turtlePrefixes": `@prefix ex: <https://example.org/id/> .
@prefix person: <https://example.org/id/person/> .`,
	}, nil)
	if analyze.StatusCode != http.StatusOK {
		t.Fatalf("analyze: %d %s", analyze.StatusCode, analyze.Body)
	}
	var an struct {
		Candidates []struct {
			IRIBase         string `json:"iriBase"`
			SuggestedCode   string `json:"suggestedCode"`
			SuggestedAction string `json:"suggestedAction"`
			TripleCount     int    `json:"tripleCount"`
		} `json:"candidates"`
		Errors []string `json:"errors"`
	}
	_ = json.Unmarshal([]byte(analyze.Body), &an)
	if len(an.Errors) > 0 || len(an.Candidates) == 0 {
		t.Fatalf("unexpected analyze: %s", analyze.Body)
	}
	cand := an.Candidates[0]
	for _, c := range an.Candidates {
		if c.IRIBase == "https://example.org/id/" {
			cand = c
			break
		}
	}
	if cand.IRIBase == "" {
		t.Fatalf("no candidates: %s", analyze.Body)
	}
	if cand.SuggestedAction != "create" {
		t.Fatalf("expected create action, got %q in %s", cand.SuggestedAction, analyze.Body)
	}

	dry := doJSON(t, h, http.MethodPost, "/v1/rdf/import", map[string]any{
		"ntriples": nt,
		"dryRun":   true,
		"assignments": []map[string]any{{
			"iriBase": cand.IRIBase, "packageCode": cand.SuggestedCode,
			"create": true, "setIriBase": true,
		}},
	}, nil)
	if dry.StatusCode != http.StatusOK {
		t.Fatalf("dry-run: %d %s", dry.StatusCode, dry.Body)
	}
	var dryRes struct {
		DryRun   bool `json:"dryRun"`
		Packages []struct {
			Action string `json:"action"`
			Import *struct {
				WouldCreate []any `json:"wouldCreate"`
			} `json:"import"`
		} `json:"packages"`
		Errors []string `json:"errors"`
	}
	_ = json.Unmarshal([]byte(dry.Body), &dryRes)
	if !dryRes.DryRun || len(dryRes.Errors) > 0 || len(dryRes.Packages) == 0 {
		t.Fatalf("unexpected dry-run: %s", dry.Body)
	}
	if dryRes.Packages[0].Action != "create" || dryRes.Packages[0].Import == nil || len(dryRes.Packages[0].Import.WouldCreate) == 0 {
		t.Fatalf("dry-run missing create plan: %s", dry.Body)
	}

	commit := doJSON(t, h, http.MethodPost, "/v1/rdf/import", map[string]any{
		"ntriples": nt,
		"dryRun":   false,
		"assignments": []map[string]any{{
			"iriBase": cand.IRIBase, "packageCode": cand.SuggestedCode,
			"create": true, "setIriBase": true, "label": "Example ontology",
		}},
	}, nil)
	if commit.StatusCode != http.StatusCreated && commit.StatusCode != http.StatusOK {
		t.Fatalf("commit: %d %s", commit.StatusCode, commit.Body)
	}

	pkg := doJSON(t, h, http.MethodGet, "/v1/packages/"+cand.SuggestedCode, nil, nil)
	if pkg.StatusCode != http.StatusOK || !strings.Contains(pkg.Body, cand.IRIBase) {
		t.Fatalf("package missing/iriBase: %d %s", pkg.StatusCode, pkg.Body)
	}

	again := doJSON(t, h, http.MethodPost, "/v1/rdf/import", map[string]any{
		"ntriples": nt,
		"dryRun":   false,
		"assignments": []map[string]any{{
			"iriBase": cand.IRIBase, "packageCode": cand.SuggestedCode,
			"create": false, "setIriBase": false,
		}},
	}, nil)
	if again.StatusCode != http.StatusCreated && again.StatusCode != http.StatusOK {
		t.Fatalf("reimport: %d %s", again.StatusCode, again.Body)
	}
	var againRes struct {
		Packages []struct {
			Import *struct {
				Matched []any `json:"matched"`
			} `json:"import"`
		} `json:"packages"`
	}
	_ = json.Unmarshal([]byte(again.Body), &againRes)
	if len(againRes.Packages) == 0 || againRes.Packages[0].Import == nil || len(againRes.Packages[0].Import.Matched) == 0 {
		t.Fatalf("expected matches on reimport: %s", again.Body)
	}

	propList := doJSON(t, h, http.MethodGet, "/v1/properties?limit=50", nil, nil)
	if propList.StatusCode != http.StatusOK {
		t.Fatalf("list properties: %d %s", propList.StatusCode, propList.Body)
	}
	if !strings.Contains(propList.Body, "Integer") {
		t.Fatalf("expected Integer property from range: %s", propList.Body)
	}
}

func truncateTestDB(t *testing.T) {
	t.Helper()
	dsn := os.Getenv("KC_DATABASE_URL")
	if dsn == "" {
		t.Skip("KC_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := store.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	_, err = pool.Exec(ctx, `
		TRUNCATE change_set_item, change_set,
			outbox_event, projection_search, projection_rdf,
			entity_iri_alias,
			release_object, release_dependency, release,
			package_dependency, package,
			lens_definition, validation_report, shape_profile,
			class_profile, property_profile,
			statement_revision_qualifier, statement_revision_reference,
			statement_qualifier, statement_reference, reference,
			statement_revision, entity_revision,
			statement_current, statement,
			entity_label, entity_description,
			entity RESTART IDENTITY CASCADE;
		UPDATE id_counter SET last_value = 0;
	`)
	if err != nil {
		t.Fatal(err)
	}
}

func createPkg(t *testing.T, h http.Handler, code string, deps []map[string]string) {
	t.Helper()
	var depList []map[string]string
	if deps != nil {
		depList = deps
	}
	body := map[string]any{
		"code": code, "lifecycle": "released",
		"labels": map[string]string{"en": code},
	}
	if len(depList) > 0 {
		body["dependencies"] = depList
	}
	res := doJSON(t, h, http.MethodPost, "/v1/packages", body, nil)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create package %s: %d %s", code, res.StatusCode, res.Body)
	}
}

func createEntityWithPkg(t *testing.T, h http.Handler, pkg, label string) string {
	t.Helper()
	res := doJSON(t, h, http.MethodPost, "/v1/entities", map[string]any{
		"packageCode": pkg,
		"labels":      map[string]string{"en": label},
	}, nil)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create entity: %d %s", res.StatusCode, res.Body)
	}
	return parseDataID(t, res.Body)
}

func createPropertyWithPkg(t *testing.T, h http.Handler, pkg, label string) string {
	t.Helper()
	res := doJSON(t, h, http.MethodPost, "/v1/properties", map[string]any{
		"packageCode": pkg,
		"datatype":    "String",
		"labels":      map[string]string{"en": label},
	}, nil)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create property: %d %s", res.StatusCode, res.Body)
	}
	return parseDataID(t, res.Body)
}

func createStatementWithPkg(t *testing.T, h http.Handler, pkg, qid, pid, val string) string {
	t.Helper()
	res := doJSON(t, h, http.MethodPost, "/v1/statements", map[string]any{
		"packageCode": pkg,
		"subject":     qid,
		"property":    pid,
		"value":       map[string]any{"type": "String", "string": val},
	}, nil)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create statement: %d %s", res.StatusCode, res.Body)
	}
	return parseDataID(t, res.Body)
}

func createEntity(t *testing.T, h http.Handler, label string) string {
	t.Helper()
	return createEntityWithPkg(t, h, "test", label)
}

func createProperty(t *testing.T, h http.Handler, label string) string {
	t.Helper()
	return createPropertyWithPkg(t, h, "test", label)
}

func createStatement(t *testing.T, h http.Handler, qid, pid, val string) string {
	t.Helper()
	return createStatementWithPkg(t, h, "test", qid, pid, val)
}

func parseDataID(t *testing.T, body string) string {
	t.Helper()
	var wrap struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &wrap); err != nil {
		t.Fatal(err)
	}
	return wrap.Data.ID
}

func parseStatementStringValue(t *testing.T, body string) string {
	t.Helper()
	var st struct {
		Value map[string]any `json:"value"`
	}
	if err := json.Unmarshal([]byte(body), &st); err != nil {
		t.Fatal(err)
	}
	s, _ := st.Value["string"].(string)
	return s
}

func parseDataField(t *testing.T, body, field, subfield string) string {
	t.Helper()
	var wrap struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &wrap); err != nil {
		t.Fatal(err)
	}
	val, _ := wrap.Data[field].(map[string]any)
	s, _ := val[subfield].(string)
	return s
}

type httpResult struct {
	StatusCode int
	Body       string
}

func doJSON(t *testing.T, h http.Handler, method, path string, body any, headers map[string]string) httpResult {
	t.Helper()
	if headers == nil {
		headers = adminHeaders()
	} else if _, ok := headers["X-Subject"]; !ok {
		for k, v := range adminHeaders() {
			if _, exists := headers[k]; !exists {
				headers[k] = v
			}
		}
	}
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, rdr)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return httpResult{StatusCode: rec.Code, Body: rec.Body.String()}
}

func TestAcceptanceSchemaValidationRelaxed(t *testing.T) {
	h := setupTestHandler(t)

	ent := doJSON(t, h, http.MethodPost, "/v1/entities", map[string]any{
		"packageCode": "test",
		"labels": map[string]string{"en": "Person"},
	}, adminHeaders())
	if ent.StatusCode != http.StatusCreated {
		t.Fatalf("create entity: %d %s", ent.StatusCode, ent.Body)
	}
	qid := parseDataID(t, ent.Body)

	typeProp := doJSON(t, h, http.MethodPost, "/v1/properties", map[string]any{
		"packageCode": "test",
		"datatype": "EntityReference",
		"labels":   map[string]string{"en": "instance of"},
	}, adminHeaders())
	pidType := parseDataID(t, typeProp.Body)

	nameProp := doJSON(t, h, http.MethodPost, "/v1/properties", map[string]any{
		"packageCode": "test",
		"datatype": "String",
		"labels":   map[string]string{"en": "name"},
		"constraints": map[string]any{
			"domainClasses": []string{"C1"},
			"severity":      "warning",
		},
	}, adminHeaders())
	pidName := parseDataID(t, nameProp.Body)

	class := doJSON(t, h, http.MethodPost, "/v1/classes", map[string]any{
		"packageCode": "test",
		"labels": map[string]string{"en": "Person"},
	}, adminHeaders())
	if class.StatusCode != http.StatusCreated {
		t.Fatalf("create class: %d %s", class.StatusCode, class.Body)
	}
	classID := parseDataID(t, class.Body)

	doJSON(t, h, http.MethodPut, "/v1/admin/schema-config", map[string]any{
		"instanceOfProperty": pidType,
	}, adminHeaders())

	doJSON(t, h, http.MethodPost, "/v1/shapes", map[string]any{
		"code":    "person-shape",
		"classId": classID,
		"document": map[string]any{
			"requiredProperties": []string{pidName},
			"severity":           "warning",
		},
	}, adminHeaders())

	doJSON(t, h, http.MethodPost, "/v1/statements", map[string]any{
		"packageCode": "test",
		"subject":  qid,
		"property": pidType,
		"value":    map[string]any{"type": "EntityReference", "entityId": classID},
	}, adminHeaders())

	// relaxed: write name without class typing would fail domain - but we typed entity above.
	// Remove instanceOf to trigger domain warning on name property usage - actually domain checks entity classes.
	// Add name statement - should pass. Add second name to test cardinality if we set maxCount.

	st := doJSON(t, h, http.MethodPost, "/v1/statements", map[string]any{
		"packageCode": "test",
		"subject":  qid,
		"property": pidName,
		"value":    map[string]any{"type": "String", "string": "Alice"},
	}, mergeHeaders(adminHeaders(), map[string]string{"X-Validation-Mode": "relaxed"}))
	if st.StatusCode != http.StatusCreated {
		t.Fatalf("create statement relaxed: %d %s", st.StatusCode, st.Body)
	}
	var wrap struct {
		Validation *struct {
			Summary struct {
				Warnings int `json:"warnings"`
				Errors   int `json:"errors"`
			} `json:"summary"`
		} `json:"validation"`
	}
	if err := json.Unmarshal([]byte(st.Body), &wrap); err != nil {
		t.Fatal(err)
	}
	if wrap.Validation == nil {
		t.Fatal("expected validation block in relaxed response")
	}

	strict := doJSON(t, h, http.MethodPost, "/v1/statements", map[string]any{
		"packageCode": "test",
		"subject":  qid,
		"property": pidName,
		"value":    map[string]any{"type": "String", "string": "Bob"},
	}, mergeHeaders(adminHeaders(), map[string]string{"X-Validation-Mode": "strict"}))
	if strict.StatusCode != http.StatusCreated {
		t.Fatalf("strict duplicate name should still pass without maxCount: %d", strict.StatusCode)
	}

	val := doJSON(t, h, http.MethodGet, "/v1/entities/"+qid+"/validation", nil, adminHeaders())
	if val.StatusCode != http.StatusOK {
		t.Fatalf("validation report: %d %s", val.StatusCode, val.Body)
	}
}

func TestAcceptanceEntityProfile(t *testing.T) {
	h := setupTestHandler(t)

	prop := doJSON(t, h, http.MethodPost, "/v1/properties", map[string]any{
		"packageCode": "test",
		"datatype": "String",
		"labels":   map[string]string{"en": "note"},
	}, adminHeaders())
	if prop.StatusCode != http.StatusCreated {
		t.Fatalf("create property: %d %s", prop.StatusCode, prop.Body)
	}
	pid := parseDataID(t, prop.Body)

	ent := doJSON(t, h, http.MethodGet, "/v1/entities/"+pid, nil, adminHeaders())
	if ent.StatusCode != http.StatusOK {
		t.Fatalf("get property as entity: %d %s", ent.StatusCode, ent.Body)
	}

	class := doJSON(t, h, http.MethodPost, "/v1/classes", map[string]any{
		"packageCode": "test",
		"labels": map[string]string{"en": "Thing"},
	}, adminHeaders())
	cid := parseDataID(t, class.Body)

	st := doJSON(t, h, http.MethodPost, "/v1/statements", map[string]any{
		"packageCode": "test",
		"subject":  pid,
		"property": pid,
		"value":    map[string]any{"type": "String", "string": "meta about property"},
	}, adminHeaders())
	if st.StatusCode != http.StatusCreated {
		t.Fatalf("statement about property: %d %s", st.StatusCode, st.Body)
	}

	qEnt := doJSON(t, h, http.MethodPost, "/v1/entities", map[string]any{
		"packageCode": "test",
		"labels": map[string]string{"en": "ordinary"},
	}, adminHeaders())
	qid := parseDataID(t, qEnt.Body)
	bad := doJSON(t, h, http.MethodPost, "/v1/statements", map[string]any{
		"packageCode": "test",
		"subject":  qid,
		"property": qid,
		"value":    map[string]any{"type": "String", "string": "no"},
	}, adminHeaders())
	if bad.StatusCode == http.StatusCreated {
		t.Fatal("expected reject when predicate has no property_profile")
	}

	_ = cid
}

func mergeHeaders(base, extra map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

func TestAcceptanceClassInReleaseAndDraft(t *testing.T) {
	h := setupTestHandler(t)

	class := doJSON(t, h, http.MethodPost, "/v1/classes", map[string]any{
		"packageCode": "test",
		"labels":      map[string]string{"en": "Person"},
	}, adminHeaders())
	if class.StatusCode != http.StatusCreated {
		t.Fatalf("create class: %d %s", class.StatusCode, class.Body)
	}
	cid := parseDataID(t, class.Body)

	pub := doJSON(t, h, http.MethodPost, "/v1/packages/test/releases", map[string]any{"version": "1.0.0"}, adminHeaders())
	if pub.StatusCode != http.StatusCreated {
		t.Fatalf("publish: %d %s", pub.StatusCode, pub.Body)
	}
	var rel struct {
		Data struct {
			Objects []struct {
				ObjectType string `json:"objectType"`
				PublicID   string `json:"publicId"`
			} `json:"objects"`
		} `json:"data"`
	}
	_ = json.Unmarshal([]byte(pub.Body), &rel)
	foundClass := false
	for _, o := range rel.Data.Objects {
		if o.ObjectType == "class" && o.PublicID == cid {
			foundClass = true
		}
	}
	if !foundClass {
		t.Fatalf("expected class %s in release objects, got %+v", cid, rel.Data.Objects)
	}

	bundle := doJSON(t, h, http.MethodGet, "/v1/packages/test/releases/1.0.0/bundle", nil, adminHeaders())
	if bundle.StatusCode != http.StatusOK {
		t.Fatalf("bundle: %d %s", bundle.StatusCode, bundle.Body)
	}
	var b struct {
		Classes []struct {
			ID string `json:"id"`
		} `json:"classes"`
	}
	_ = json.Unmarshal([]byte(bundle.Body), &b)
	if len(b.Classes) != 1 || b.Classes[0].ID != cid {
		t.Fatalf("bundle classes: %+v", b.Classes)
	}

	// Draft workflow
	put := doJSON(t, h, http.MethodPut, "/v1/me/changeset-draft", map[string]any{
		"open": true, "title": "batch", "packageCode": "test",
		"operations": []map[string]any{
			{"op": "createEntity", "clientKey": "$e1", "packageCode": "test", "labels": map[string]string{"en": "Drafted"}},
		},
	}, adminHeaders())
	if put.StatusCode != http.StatusOK {
		t.Fatalf("put draft: %d %s", put.StatusCode, put.Body)
	}
	get := doJSON(t, h, http.MethodGet, "/v1/me/changeset-draft", nil, adminHeaders())
	if get.StatusCode != http.StatusOK {
		t.Fatalf("get draft: %d %s", get.StatusCode, get.Body)
	}
	var draft struct {
		Open       bool  `json:"open"`
		Operations []any `json:"operations"`
	}
	_ = json.Unmarshal([]byte(get.Body), &draft)
	if !draft.Open || len(draft.Operations) != 1 {
		t.Fatalf("draft state: %+v", draft)
	}
	commit := doJSON(t, h, http.MethodPost, "/v1/me/changeset-draft/commit", map[string]any{}, adminHeaders())
	if commit.StatusCode != http.StatusOK {
		t.Fatalf("commit draft: %d %s", commit.StatusCode, commit.Body)
	}
	get2 := doJSON(t, h, http.MethodGet, "/v1/me/changeset-draft", nil, adminHeaders())
	_ = json.Unmarshal([]byte(get2.Body), &draft)
	if draft.Open {
		t.Fatal("draft should be closed after commit")
	}
	ents := doJSON(t, h, http.MethodGet, "/v1/entities?q=Drafted", nil, adminHeaders())
	var list struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	_ = json.Unmarshal([]byte(ents.Body), &list)
	if len(list.Items) == 0 {
		t.Fatal("expected drafted entity after commit")
	}
}
