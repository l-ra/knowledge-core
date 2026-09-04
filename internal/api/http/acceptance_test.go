package apihttp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	"github.com/l-ra/knowledge-core/internal/testdata"
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
			entity, auth_runtime,
			changeset_statement_overlay, changeset_entity_overlay, changeset_object_claim
			RESTART IDENTITY CASCADE;
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
		"datatype":    "EntityReference",
		"labels":      map[string]string{"en": "ownerRef"},
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
		"subject":     app,
		"property":    pOwner,
		"value":       map[string]any{"type": "EntityReference", "entityId": owner},
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
		"labels":      map[string]string{"cs": "Zákazník"},
	}, nil)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("A2: want 400, got %d %s", res.StatusCode, res.Body)
	}

	ent := doJSON(t, h, http.MethodPost, "/v1/entities", map[string]any{
		"packageCode": "test",
		"labels":      map[string]string{"en": "Customer"},
	}, nil)
	if ent.StatusCode != http.StatusCreated {
		t.Fatalf("create entity: %d %s", ent.StatusCode, ent.Body)
	}
	qid := parseDataID(t, ent.Body)

	prop := doJSON(t, h, http.MethodPost, "/v1/properties", map[string]any{
		"packageCode": "test",
		"datatype":    "String",
		"labels":      map[string]string{"en": "Code"},
	}, nil)
	if prop.StatusCode != http.StatusCreated {
		t.Fatalf("create property: %d %s", prop.StatusCode, prop.Body)
	}
	pid := parseDataID(t, prop.Body)

	stmt := doJSON(t, h, http.MethodPost, "/v1/statements", map[string]any{
		"packageCode": "test",
		"subject":     qid,
		"property":    pid,
		"value":       map[string]any{"type": "String", "string": "CRM-01"},
	}, nil)
	if stmt.StatusCode != http.StatusCreated {
		t.Fatalf("create statement: %d %s", stmt.StatusCode, stmt.Body)
	}
	sid := parseDataID(t, stmt.Body)

	got := doJSON(t, h, http.MethodGet, statementPath(sid, ""), nil, nil)
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
	conflict := doJSON(t, h, http.MethodPost, statementPath(s1, "/revise"), map[string]any{
		"expectedRevision": 1,
		"value":            map[string]any{"type": "String", "string": "A-first"},
	}, nil)
	if conflict.StatusCode != http.StatusOK {
		t.Fatalf("first revise: %d %s", conflict.StatusCode, conflict.Body)
	}
	stale := doJSON(t, h, http.MethodPost, statementPath(s1, "/revise"), map[string]any{
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
	s2got := doJSON(t, h, http.MethodGet, statementPath(s2, ""), nil, nil)
	s3got := doJSON(t, h, http.MethodGet, statementPath(s3, ""), nil, nil)
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
	first := doJSON(t, h, http.MethodPost, statementPath(s1, "/revise"), body, map[string]string{"Idempotency-Key": idemKey})
	second := doJSON(t, h, http.MethodPost, statementPath(s1, "/revise"), body, map[string]string{"Idempotency-Key": idemKey})
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

	list := doJSON(t, h, http.MethodGet, "/v1/changesets?limit=50", nil, adminHeaders())
	if list.StatusCode != http.StatusOK {
		t.Fatalf("list changesets: %d %s", list.StatusCode, list.Body)
	}
	var listed struct {
		Items []struct {
			ID            string `json:"id"`
			Actor         string `json:"actor"`
			OperationType string `json:"operationType"`
			ItemCount     int    `json:"itemCount"`
		} `json:"items"`
		NextCursor string `json:"nextCursor"`
	}
	_ = json.Unmarshal([]byte(list.Body), &listed)
	if len(listed.Items) == 0 {
		t.Fatal("list changesets: expected at least one item")
	}
	found := false
	for _, it := range listed.Items {
		if it.ID == cs1.ChangeSet.ID {
			found = true
			if it.ItemCount < 1 {
				t.Fatalf("list item expected itemCount>=1: %+v", it)
			}
			break
		}
	}
	if !found {
		t.Fatalf("list changesets: expected %s in results, got %+v", cs1.ChangeSet.ID, listed.Items)
	}
	detail := doJSON(t, h, http.MethodGet, "/v1/changesets/"+cs1.ChangeSet.ID, nil, adminHeaders())
	if detail.StatusCode != http.StatusOK {
		t.Fatalf("get changeset: %d %s", detail.StatusCode, detail.Body)
	}
	var det struct {
		ID    string `json:"id"`
		Items []any  `json:"items"`
	}
	_ = json.Unmarshal([]byte(detail.Body), &det)
	if det.ID != cs1.ChangeSet.ID || len(det.Items) == 0 {
		t.Fatalf("get changeset detail: %+v", det)
	}
	objFilter := doJSON(t, h, http.MethodGet, "/v1/changesets?objectId="+url.QueryEscape(s1)+"&limit=10", nil, adminHeaders())
	if objFilter.StatusCode != http.StatusOK {
		t.Fatalf("list by objectId: %d %s", objFilter.StatusCode, objFilter.Body)
	}
	var byObj struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	_ = json.Unmarshal([]byte(objFilter.Body), &byObj)
	foundObj := false
	for _, it := range byObj.Items {
		if it.ID == cs1.ChangeSet.ID {
			foundObj = true
			break
		}
	}
	if !foundObj {
		t.Fatalf("list by objectId=%s: expected %s, got %+v", s1, cs1.ChangeSet.ID, byObj.Items)
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
		"packageCode":  "test",
		"subject":      qid,
		"property":     ownerProp,
		"value":        map[string]any{"type": "String", "string": "owner-1"},
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
	rev := doJSON(t, h, http.MethodPost, statementPath(sid, "/revise"), map[string]any{
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
	hist := doJSON(t, h, http.MethodGet, statementPath(sid, "/history"), nil, nil)
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

	st := doJSON(t, h, http.MethodGet, statementPath(sid, ""), nil, nil)
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

func TestAcceptancePackageUpgradeCompat(t *testing.T) {
	h := setupTestHandler(t)

	createPkg(t, h, "meta-pkg", nil)
	qid := createEntityWithPkg(t, h, "meta-pkg", "Policy Entity")
	pid := doJSON(t, h, http.MethodPost, "/v1/properties", map[string]any{
		"packageCode": "meta-pkg",
		"datatype":    "String",
		"iriLocal":    "note",
		"labels":      map[string]string{"en": "note"},
		"constraints": map[string]any{"minCount": 0, "maxCount": 1},
	}, nil)
	if pid.StatusCode != http.StatusCreated {
		t.Fatalf("create property: %d %s", pid.StatusCode, pid.Body)
	}
	propID := parseDataID(t, pid.Body)
	sid := createStatementWithPkg(t, h, "meta-pkg", qid, propID, "v1")

	pub := doJSON(t, h, http.MethodPost, "/v1/packages/meta-pkg/releases", map[string]any{"version": "1.0.0"}, nil)
	if pub.StatusCode != http.StatusCreated {
		t.Fatalf("publish 1.0.0: %d %s", pub.StatusCode, pub.Body)
	}
	bundleRes := doJSON(t, h, http.MethodGet, "/v1/packages/meta-pkg/releases/1.0.0/bundle", nil, nil)
	if bundleRes.StatusCode != http.StatusOK {
		t.Fatalf("export: %d %s", bundleRes.StatusCode, bundleRes.Body)
	}

	var bundle map[string]any
	if err := json.Unmarshal([]byte(bundleRes.Body), &bundle); err != nil {
		t.Fatal(err)
	}

	truncateTestDB(t)

	imp := doJSON(t, h, http.MethodPost, "/v1/releases/import", bundle, nil)
	if imp.StatusCode != http.StatusCreated {
		t.Fatalf("import 1.0.0: %d %s", imp.StatusCode, imp.Body)
	}

	// Additive upgrade 1.1.0: new property + bump statement revision (metadata value change).
	up := cloneJSONMap(t, bundle)
	up["manifest"].(map[string]any)["version"] = "1.1.0"
	if releases, ok := up["releases"].([]any); ok && len(releases) > 0 {
		releases[0].(map[string]any)["version"] = "1.1.0"
	}
	newProp := map[string]any{
		"id":           "urn:kc:meta-pkg:p_extra_upgrade",
		"packageCode":  "meta-pkg",
		"iriLocal":     "extra",
		"revisionNo":   1,
		"datatype":     "String",
		"status":       "active",
		"labels":       map[string]any{"en": "extra"},
		"descriptions": map[string]any{},
	}
	props := up["properties"].([]any)
	up["properties"] = append(props, newProp)
	stmts := up["statements"].([]any)
	for i, raw := range stmts {
		st := raw.(map[string]any)
		if st["id"] == sid {
			st["revisionNo"] = 2
			st["value"] = map[string]any{"type": "String", "string": "v1.1"}
			stmts[i] = st
		}
	}
	up["statements"] = stmts
	idx := up["manifest"].(map[string]any)["objectIndex"].([]any)
	idx = append(idx, map[string]any{
		"objectType": "property", "objectPublicId": newProp["id"], "revisionNo": 1,
	})
	for i, raw := range idx {
		o := raw.(map[string]any)
		if o["objectPublicId"] == sid {
			o["revisionNo"] = 2
			idx[i] = o
		}
	}
	up["manifest"].(map[string]any)["objectIndex"] = idx
	if releases, ok := up["releases"].([]any); ok && len(releases) > 0 {
		releases[0].(map[string]any)["objectIndex"] = idx
		releases[0].(map[string]any)["version"] = "1.1.0"
	}

	imp2 := doJSON(t, h, http.MethodPost, "/v1/releases/import", up, nil)
	if imp2.StatusCode != http.StatusCreated {
		t.Fatalf("import 1.1.0 additive: %d %s", imp2.StatusCode, imp2.Body)
	}
	gotSt := doJSON(t, h, http.MethodGet, statementPath(sid, ""), nil, nil)
	if gotSt.StatusCode != http.StatusOK {
		t.Fatalf("statement after upgrade: %d %s", gotSt.StatusCode, gotSt.Body)
	}
	var stBody map[string]any
	_ = json.Unmarshal([]byte(gotSt.Body), &stBody)
	if rev, _ := stBody["revisionNo"].(float64); int(rev) != 2 {
		t.Fatalf("expected statement rev 2, got %v body=%s", stBody["revisionNo"], gotSt.Body)
	}

	// Breaking upgrade: tighten minCount on existing property.
	brk := cloneJSONMap(t, up)
	brk["manifest"].(map[string]any)["version"] = "1.2.0"
	if releases, ok := brk["releases"].([]any); ok && len(releases) > 0 {
		releases[0].(map[string]any)["version"] = "1.2.0"
	}
	for i, raw := range brk["properties"].([]any) {
		p := raw.(map[string]any)
		if p["id"] == propID {
			p["revisionNo"] = float64(2)
			p["constraints"] = map[string]any{"minCount": 1, "maxCount": 1}
			brk["properties"].([]any)[i] = p
		}
	}
	imp3 := doJSON(t, h, http.MethodPost, "/v1/releases/import", brk, nil)
	if imp3.StatusCode != http.StatusConflict {
		t.Fatalf("breaking import: want 409, got %d %s", imp3.StatusCode, imp3.Body)
	}
	if !strings.Contains(imp3.Body, "compat_breaking") {
		t.Fatalf("expected compat_breaking code, got %s", imp3.Body)
	}

	dup := doJSON(t, h, http.MethodPost, "/v1/releases/import", up, nil)
	if dup.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate 1.1.0 import: want 409, got %d %s", dup.StatusCode, dup.Body)
	}
}

func TestAcceptancePublishCompatBreaking(t *testing.T) {
	h := setupTestHandler(t)
	createPkg(t, h, "pub-pkg", nil)
	createPkg(t, h, "other-pkg", nil)
	_ = createEntityWithPkg(t, h, "pub-pkg", "Keep")
	moved := createEntityWithPkg(t, h, "pub-pkg", "WillMove")

	pub1 := doJSON(t, h, http.MethodPost, "/v1/packages/pub-pkg/releases", map[string]any{"version": "1.0.0"}, nil)
	if pub1.StatusCode != http.StatusCreated {
		t.Fatalf("publish 1.0.0: %d %s", pub1.StatusCode, pub1.Body)
	}

	mv := doJSON(t, h, http.MethodPost, entityPath(moved, "/move"), map[string]any{
		"packageCode": "other-pkg",
	}, nil)
	if mv.StatusCode != http.StatusOK {
		t.Fatalf("move entity out of package: %d %s", mv.StatusCode, mv.Body)
	}

	pub2 := doJSON(t, h, http.MethodPost, "/v1/packages/pub-pkg/releases", map[string]any{"version": "1.1.0"}, nil)
	if pub2.StatusCode != http.StatusConflict {
		t.Fatalf("publish breaking: want 409, got %d %s", pub2.StatusCode, pub2.Body)
	}
	if !strings.Contains(pub2.Body, "compat_breaking") {
		t.Fatalf("expected compat_breaking, got %s", pub2.Body)
	}

	// Additive publish: move entity back, add property, publish.
	mvBack := doJSON(t, h, http.MethodPost, entityPath(moved, "/move"), map[string]any{
		"packageCode": "pub-pkg",
	}, nil)
	if mvBack.StatusCode != http.StatusOK {
		t.Fatalf("move back: %d %s", mvBack.StatusCode, mvBack.Body)
	}
	add := doJSON(t, h, http.MethodPost, "/v1/properties", map[string]any{
		"packageCode": "pub-pkg",
		"datatype":    "String",
		"iriLocal":    "extra",
		"labels":      map[string]string{"en": "extra"},
	}, nil)
	if add.StatusCode != http.StatusCreated {
		t.Fatalf("add property: %d %s", add.StatusCode, add.Body)
	}
	pub3 := doJSON(t, h, http.MethodPost, "/v1/packages/pub-pkg/releases", map[string]any{"version": "1.1.0"}, nil)
	if pub3.StatusCode != http.StatusCreated {
		t.Fatalf("publish additive 1.1.0: %d %s", pub3.StatusCode, pub3.Body)
	}

	clean := doJSON(t, h, http.MethodGet, "/v1/packages", nil, nil)
	if clean.StatusCode != http.StatusOK {
		t.Fatalf("list packages: %d %s", clean.StatusCode, clean.Body)
	}
	if !strings.Contains(clean.Body, `"code":"pub-pkg"`) || !strings.Contains(clean.Body, `"latestReleaseVersion":"1.1.0"`) {
		t.Fatalf("expected pub-pkg@1.1.0 in list: %s", clean.Body)
	}
	if strings.Contains(clean.Body, `"modifiedAfterRelease":true`) {
		t.Fatalf("just-published package should be clean: %s", clean.Body)
	}

	extra2 := doJSON(t, h, http.MethodPost, "/v1/entities", map[string]any{
		"packageCode": "pub-pkg",
		"labels":      map[string]string{"en": "AfterRelease"},
	}, nil)
	if extra2.StatusCode != http.StatusCreated {
		t.Fatalf("create entity after release: %d %s", extra2.StatusCode, extra2.Body)
	}
	dirty := doJSON(t, h, http.MethodGet, "/v1/packages", nil, nil)
	if dirty.StatusCode != http.StatusOK {
		t.Fatalf("list packages dirty: %d %s", dirty.StatusCode, dirty.Body)
	}
	if !strings.Contains(dirty.Body, `"modifiedAfterRelease":true`) {
		t.Fatalf("expected modifiedAfterRelease after new entity: %s", dirty.Body)
	}
}

func cloneJSONMap(t *testing.T, in map[string]any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestAcceptanceIRIFirstIdentityBundle(t *testing.T) {
	h := setupTestHandler(t)

	urnEnt := doJSON(t, h, http.MethodPost, "/v1/entities", map[string]any{
		"packageCode": "test",
		"labels":      map[string]string{"en": "URN Entity"},
	}, nil)
	if urnEnt.StatusCode != http.StatusCreated {
		t.Fatalf("create urn entity: %d %s", urnEnt.StatusCode, urnEnt.Body)
	}
	urnID := parseDataID(t, urnEnt.Body)
	if !regexp.MustCompile(`^urn:kc:test:e_[0-9a-z]+$`).MatchString(urnID) {
		t.Fatalf("expected URN entity id, got %q", urnID)
	}
	gotURN := doJSON(t, h, http.MethodGet, entityPath(urnID, ""), nil, nil)
	if gotURN.StatusCode != http.StatusOK {
		t.Fatalf("get urn entity by id: %d %s", gotURN.StatusCode, gotURN.Body)
	}

	res := doJSON(t, h, http.MethodPost, "/v1/packages", map[string]any{
		"code":      "archimate-lite",
		"lifecycle": "released",
		"iriBase":   "https://example.org/archimate-lite/",
		"labels":    map[string]string{"en": "archimate-lite"},
	}, nil)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create package: %d %s", res.StatusCode, res.Body)
	}

	ent := doJSON(t, h, http.MethodPost, "/v1/entities", map[string]any{
		"packageCode": "archimate-lite",
		"iriLocal":    "BusinessObject",
		"labels":      map[string]string{"en": "Business Object"},
	}, nil)
	if ent.StatusCode != http.StatusCreated {
		t.Fatalf("create entity: %d %s", ent.StatusCode, ent.Body)
	}
	qid := parseDataID(t, ent.Body)
	if qid != "https://example.org/archimate-lite/BusinessObject" {
		t.Fatalf("expected IRI entity id, got %q", qid)
	}

	fallbackEnt := doJSON(t, h, http.MethodPost, "/v1/entities", map[string]any{
		"packageCode": "archimate-lite",
		"labels":      map[string]string{"en": "Generated Object"},
	}, nil)
	if fallbackEnt.StatusCode != http.StatusCreated {
		t.Fatalf("create fallback entity: %d %s", fallbackEnt.StatusCode, fallbackEnt.Body)
	}
	var fallbackEntity struct {
		Data struct {
			ID        string `json:"id"`
			DisplayID string `json:"displayId"`
			IRILocal  string `json:"iriLocal"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(fallbackEnt.Body), &fallbackEntity); err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^https://example\.org/archimate-lite/e_[0-9a-z]+$`).MatchString(fallbackEntity.Data.ID) {
		t.Fatalf("expected snowflake fallback entity IRI, got %q", fallbackEntity.Data.ID)
	}
	if !regexp.MustCompile(`^archimate-lite:e_[0-9a-z]+$`).MatchString(fallbackEntity.Data.DisplayID) {
		t.Fatalf("expected snowflake display id, got %q", fallbackEntity.Data.DisplayID)
	}
	if !regexp.MustCompile(`^e_[0-9a-z]+$`).MatchString(fallbackEntity.Data.IRILocal) {
		t.Fatalf("expected snowflake iriLocal, got %q", fallbackEntity.Data.IRILocal)
	}

	prop := doJSON(t, h, http.MethodPost, "/v1/properties", map[string]any{
		"packageCode": "archimate-lite",
		"iriLocal":    "modelingDepth",
		"datatype":    "String",
		"labels":      map[string]string{"en": "Modeling depth"},
	}, nil)
	if prop.StatusCode != http.StatusCreated {
		t.Fatalf("create property: %d %s", prop.StatusCode, prop.Body)
	}
	pid := parseDataID(t, prop.Body)
	if pid != "https://example.org/archimate-lite/modelingDepth" {
		t.Fatalf("expected IRI property id, got %q", pid)
	}

	stmt := doJSON(t, h, http.MethodPost, "/v1/statements", map[string]any{
		"packageCode": "archimate-lite",
		"subject":     qid,
		"property":    pid,
		"value":       map[string]any{"type": "String", "string": "catalog"},
	}, nil)
	if stmt.StatusCode != http.StatusCreated {
		t.Fatalf("create statement: %d %s", stmt.StatusCode, stmt.Body)
	}
	sid := parseDataID(t, stmt.Body)
	if !regexp.MustCompile(`^https://example\.org/archimate-lite/statement/s_[0-9a-z]+$`).MatchString(sid) &&
		!regexp.MustCompile(`^urn:kc:archimate-lite:statement:s_[0-9a-z]+$`).MatchString(sid) {
		t.Fatalf("expected IRI-like statement id, got %q", sid)
	}

	ref := doJSON(t, h, http.MethodPost, "/v1/references", map[string]any{
		"fields": map[string]any{"sourceUrl": "https://example.org/doc"},
	}, nil)
	if ref.StatusCode != http.StatusCreated {
		t.Fatalf("create reference: %d %s", ref.StatusCode, ref.Body)
	}
	rid := parseDataID(t, ref.Body)
	if !regexp.MustCompile(`^urn:kc:reference:r_[0-9a-z]+$`).MatchString(rid) {
		t.Fatalf("expected snowflake reference id, got %q", rid)
	}

	pub := doJSON(t, h, http.MethodPost, "/v1/packages/archimate-lite/releases", map[string]any{"version": "1.0.0"}, nil)
	if pub.StatusCode != http.StatusCreated {
		t.Fatalf("publish: %d %s", pub.StatusCode, pub.Body)
	}

	bundle := doJSON(t, h, http.MethodGet, "/v1/packages/archimate-lite/releases/1.0.0/bundle", nil, nil)
	if bundle.StatusCode != http.StatusOK {
		t.Fatalf("bundle: %d %s", bundle.StatusCode, bundle.Body)
	}
	var b struct {
		Entities []struct {
			ID       string `json:"id"`
			IRILocal string `json:"iriLocal"`
		} `json:"entities"`
		Properties []struct {
			ID       string `json:"id"`
			IRILocal string `json:"iriLocal"`
		} `json:"properties"`
	}
	if err := json.Unmarshal([]byte(bundle.Body), &b); err != nil {
		t.Fatal(err)
	}
	if len(b.Entities) < 1 {
		t.Fatalf("expected at least one entity in bundle, got %+v", b.Entities)
	}
	var foundEntity bool
	for _, e := range b.Entities {
		if e.ID == qid && e.IRILocal == "BusinessObject" {
			foundEntity = true
			break
		}
	}
	if !foundEntity {
		t.Fatalf("expected BusinessObject entity in bundle, got %+v", b.Entities)
	}
	if len(b.Properties) != 1 || b.Properties[0].ID != pid || b.Properties[0].IRILocal != "modelingDepth" {
		t.Fatalf("unexpected property bundle payload: %+v", b.Properties)
	}

	got := doJSON(t, h, http.MethodGet, entityPath(qid, ""), nil, nil)
	if got.StatusCode != http.StatusOK {
		t.Fatalf("get entity by iri: %d %s", got.StatusCode, got.Body)
	}
}

func TestAcceptanceEntityLifecycleDeprecateDelete(t *testing.T) {
	h := setupTestHandler(t)

	lonely := createEntity(t, h, "Lifecycle Lonely")
	dep := doJSON(t, h, http.MethodPost, entityPath(lonely, "/deprecate"), map[string]any{"expectedRevision": 1}, nil)
	if dep.StatusCode != http.StatusOK {
		t.Fatalf("deprecate: %d %s", dep.StatusCode, dep.Body)
	}
	got := doJSON(t, h, http.MethodGet, entityPath(lonely, ""), nil, nil)
	if got.StatusCode != http.StatusOK {
		t.Fatalf("deprecated entity GET: %d %s", got.StatusCode, got.Body)
	}
	var ent struct {
		Status     string `json:"status"`
		RevisionNo int    `json:"revisionNo"`
	}
	if err := json.Unmarshal([]byte(got.Body), &ent); err != nil {
		t.Fatal(err)
	}
	if ent.Status != "deprecated" || ent.RevisionNo != 2 {
		t.Fatalf("expected deprecated rev 2, got %+v", ent)
	}
	again := doJSON(t, h, http.MethodPost, entityPath(lonely, "/deprecate"), map[string]any{"expectedRevision": 2}, nil)
	if again.StatusCode != http.StatusConflict {
		t.Fatalf("re-deprecate want 409, got %d %s", again.StatusCode, again.Body)
	}
	listed := doJSON(t, h, http.MethodGet, "/v1/entities?q="+url.QueryEscape("Lifecycle Lonely"), nil, nil)
	if listed.StatusCode != http.StatusOK || !strings.Contains(listed.Body, lonely) {
		t.Fatalf("deprecated entity should remain listed: %d %s", listed.StatusCode, listed.Body)
	}

	del := doJSON(t, h, http.MethodPost, entityPath(lonely, "/delete"), map[string]any{"expectedRevision": 2}, nil)
	if del.StatusCode != http.StatusOK {
		t.Fatalf("delete: %d %s", del.StatusCode, del.Body)
	}
	gone := doJSON(t, h, http.MethodGet, entityPath(lonely, ""), nil, nil)
	if gone.StatusCode != http.StatusNotFound {
		t.Fatalf("deleted entity GET want 404, got %d %s", gone.StatusCode, gone.Body)
	}
	hidden := doJSON(t, h, http.MethodGet, "/v1/entities?q="+url.QueryEscape("Lifecycle Lonely"), nil, nil)
	if hidden.StatusCode != http.StatusOK || strings.Contains(hidden.Body, lonely) {
		t.Fatalf("deleted entity should not be listed: %d %s", hidden.StatusCode, hidden.Body)
	}
	hist := doJSON(t, h, http.MethodGet, entityPath(lonely, "/history"), nil, nil)
	if hist.StatusCode != http.StatusOK || !strings.Contains(hist.Body, `"deleted"`) {
		t.Fatalf("history should keep deleted revision: %d %s", hist.StatusCode, hist.Body)
	}

	subject := createEntity(t, h, "Lifecycle Subject")
	target := createEntity(t, h, "Lifecycle Target")
	pid := doJSON(t, h, http.MethodPost, "/v1/properties", map[string]any{
		"packageCode": "test", "datatype": "EntityReference",
		"labels": map[string]string{"en": "Lifecycle Rel"},
	}, nil)
	if pid.StatusCode != http.StatusCreated {
		t.Fatalf("create rel property: %d %s", pid.StatusCode, pid.Body)
	}
	relPID := parseDataID(t, pid.Body)
	out := doJSON(t, h, http.MethodPost, "/v1/statements", map[string]any{
		"packageCode": "test", "subject": subject, "property": relPID,
		"value": map[string]any{"type": "EntityReference", "entityId": target},
	}, nil)
	if out.StatusCode != http.StatusCreated {
		t.Fatalf("incoming statement: %d %s", out.StatusCode, out.Body)
	}
	inSID := parseDataID(t, out.Body)

	blocked := doJSON(t, h, http.MethodPost, entityPath(target, "/delete"), map[string]any{"expectedRevision": 1}, nil)
	if blocked.StatusCode != http.StatusConflict {
		t.Fatalf("delete referenced entity want 409, got %d %s", blocked.StatusCode, blocked.Body)
	}

	depSt := doJSON(t, h, http.MethodPost, statementPath(inSID, "/deprecate"), map[string]any{"expectedRevision": 1}, nil)
	if depSt.StatusCode != http.StatusOK {
		t.Fatalf("deprecate incoming: %d %s", depSt.StatusCode, depSt.Body)
	}
	stGone := doJSON(t, h, http.MethodGet, "/v1/statements?subject="+url.QueryEscape(subject), nil, nil)
	if stGone.StatusCode != http.StatusOK || strings.Contains(stGone.Body, inSID) {
		t.Fatalf("deprecated statement should not list: %d %s", stGone.StatusCode, stGone.Body)
	}

	okDel := doJSON(t, h, http.MethodPost, entityPath(target, "/delete"), map[string]any{"expectedRevision": 1}, nil)
	if okDel.StatusCode != http.StatusOK {
		t.Fatalf("delete after incoming deprecated: %d %s", okDel.StatusCode, okDel.Body)
	}

	withOut := createEntity(t, h, "Lifecycle With Outgoing")
	namePID := createProperty(t, h, "Lifecycle Name")
	outName := createStatement(t, h, withOut, namePID, "hello")
	cs := doJSON(t, h, http.MethodPost, "/v1/changesets", map[string]any{
		"operations": []map[string]any{
			{"op": "deleteEntity", "entity": withOut, "expectedRevision": 1},
		},
	}, nil)
	if cs.StatusCode != http.StatusOK && cs.StatusCode != http.StatusCreated {
		t.Fatalf("changeset deleteEntity: %d %s", cs.StatusCode, cs.Body)
	}
	if got := doJSON(t, h, http.MethodGet, entityPath(withOut, ""), nil, nil); got.StatusCode != http.StatusNotFound {
		t.Fatalf("changeset-deleted entity want 404, got %d %s", got.StatusCode, got.Body)
	}
	st := doJSON(t, h, http.MethodGet, statementPath(outName, ""), nil, nil)
	if st.StatusCode != http.StatusOK || !strings.Contains(st.Body, `"deprecated"`) {
		t.Fatalf("outgoing statement should be deprecated: %d %s", st.StatusCode, st.Body)
	}
}

func TestAcceptanceDeletePackagePurgesOwnedGraph(t *testing.T) {
	h := setupTestHandler(t)

	createPkg(t, h, "pkg-a", nil)
	createPkg(t, h, "pkg-b", nil)

	qid := createEntityWithPkg(t, h, "pkg-a", "Owned entity")
	pid := createPropertyWithPkg(t, h, "pkg-b", "External prop")
	sid := createStatementWithPkg(t, h, "pkg-b", qid, pid, "linked")

	del := doJSON(t, h, http.MethodDelete, "/v1/packages/pkg-a", nil, nil)
	if del.StatusCode != http.StatusOK {
		t.Fatalf("delete package: %d %s", del.StatusCode, del.Body)
	}

	pkg := doJSON(t, h, http.MethodGet, "/v1/packages/pkg-a", nil, nil)
	if pkg.StatusCode != http.StatusNotFound {
		t.Fatalf("deleted package should 404, got %d %s", pkg.StatusCode, pkg.Body)
	}

	ent := doJSON(t, h, http.MethodGet, entityPath(qid, ""), nil, nil)
	if ent.StatusCode != http.StatusNotFound {
		t.Fatalf("deleted package entity should 404, got %d %s", ent.StatusCode, ent.Body)
	}

	st := doJSON(t, h, http.MethodGet, statementPath(sid, ""), nil, nil)
	if st.StatusCode != http.StatusNotFound {
		t.Fatalf("statement referencing deleted package should 404, got %d %s", st.StatusCode, st.Body)
	}

	prop := doJSON(t, h, http.MethodGet, propertyPath(pid, ""), nil, nil)
	if prop.StatusCode != http.StatusOK {
		t.Fatalf("other package property should remain, got %d %s", prop.StatusCode, prop.Body)
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

	rev := doJSON(t, h, http.MethodPost, statementPath(sName, "/revise"), map[string]any{
		"expectedRevision": 1,
		"value":            map[string]any{"type": "String", "string": "Beta"},
	}, limited)
	if rev.StatusCode != http.StatusOK {
		t.Fatalf("A4: revise name: %d %s", rev.StatusCode, rev.Body)
	}

	secretAfter := doJSON(t, h, http.MethodGet, statementPath(sSecret, ""), nil, admin)
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
	secretRead := doJSON(t, h, http.MethodGet, statementPath(sSecret, ""), nil, userHeaders("U-viewer", "viewer"))
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

	get := doJSON(t, h, http.MethodGet, entityPath(qid, ""), nil, nil)
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

	alias := doJSON(t, h, http.MethodPut, entityPath(qid, "/iri-aliases"), map[string]any{
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

func statementPath(id string, suffix string) string {
	return resourcePath("/v1/statements/", id, suffix)
}

func entityPath(id string, suffix string) string {
	return resourcePath("/v1/entities/", id, suffix)
}

func propertyPath(id string, suffix string) string {
	return resourcePath("/v1/properties/", id, suffix)
}

func resourcePath(prefix, id, suffix string) string {
	return prefix + url.PathEscape(id) + suffix
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
		"labels":      map[string]string{"en": "Person"},
	}, adminHeaders())
	if ent.StatusCode != http.StatusCreated {
		t.Fatalf("create entity: %d %s", ent.StatusCode, ent.Body)
	}
	qid := parseDataID(t, ent.Body)

	typeProp := doJSON(t, h, http.MethodPost, "/v1/properties", map[string]any{
		"packageCode": "test",
		"datatype":    "EntityReference",
		"labels":      map[string]string{"en": "instance of"},
	}, adminHeaders())
	pidType := parseDataID(t, typeProp.Body)

	nameProp := doJSON(t, h, http.MethodPost, "/v1/properties", map[string]any{
		"packageCode": "test",
		"datatype":    "String",
		"labels":      map[string]string{"en": "name"},
		"constraints": map[string]any{
			"domainClasses": []string{"C1"},
			"severity":      "warning",
		},
	}, adminHeaders())
	pidName := parseDataID(t, nameProp.Body)

	class := doJSON(t, h, http.MethodPost, "/v1/classes", map[string]any{
		"packageCode": "test",
		"labels":      map[string]string{"en": "Person"},
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
		"subject":     qid,
		"property":    pidType,
		"value":       map[string]any{"type": "EntityReference", "entityId": classID},
	}, adminHeaders())

	// relaxed: write name without class typing would fail domain - but we typed entity above.
	// Remove instanceOf to trigger domain warning on name property usage - actually domain checks entity classes.
	// Add name statement - should pass. Add second name to test cardinality if we set maxCount.

	st := doJSON(t, h, http.MethodPost, "/v1/statements", map[string]any{
		"packageCode": "test",
		"subject":     qid,
		"property":    pidName,
		"value":       map[string]any{"type": "String", "string": "Alice"},
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
		"subject":     qid,
		"property":    pidName,
		"value":       map[string]any{"type": "String", "string": "Bob"},
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
		"datatype":    "String",
		"labels":      map[string]string{"en": "note"},
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
		"labels":      map[string]string{"en": "Thing"},
	}, adminHeaders())
	cid := parseDataID(t, class.Body)

	st := doJSON(t, h, http.MethodPost, "/v1/statements", map[string]any{
		"packageCode": "test",
		"subject":     pid,
		"property":    pid,
		"value":       map[string]any{"type": "String", "string": "meta about property"},
	}, adminHeaders())
	if st.StatusCode != http.StatusCreated {
		t.Fatalf("statement about property: %d %s", st.StatusCode, st.Body)
	}

	qEnt := doJSON(t, h, http.MethodPost, "/v1/entities", map[string]any{
		"packageCode": "test",
		"labels":      map[string]string{"en": "ordinary"},
	}, adminHeaders())
	qid := parseDataID(t, qEnt.Body)
	bad := doJSON(t, h, http.MethodPost, "/v1/statements", map[string]any{
		"packageCode": "test",
		"subject":     qid,
		"property":    qid,
		"value":       map[string]any{"type": "String", "string": "no"},
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

	// Open ChangeSet workflow
	open := doJSON(t, h, http.MethodPost, "/v1/changesets/open", map[string]any{"comment": "batch"}, adminHeaders())
	if open.StatusCode != http.StatusCreated {
		t.Fatalf("open changeset: %d %s", open.StatusCode, open.Body)
	}
	csID := parseDataID(t, open.Body)
	writeHeaders := adminHeaders()
	writeHeaders["X-Knowledge-Changeset"] = csID
	create := doJSON(t, h, http.MethodPost, "/v1/entities", map[string]any{
		"packageCode": "test",
		"labels":      map[string]string{"en": "Drafted"},
	}, writeHeaders)
	if create.StatusCode != http.StatusCreated {
		t.Fatalf("create in open cs: %d %s", create.StatusCode, create.Body)
	}
	eid := parseDataID(t, create.Body)

	without := doJSON(t, h, http.MethodGet, "/v1/entities/"+url.PathEscape(eid), nil, adminHeaders())
	if without.StatusCode != http.StatusNotFound {
		t.Fatalf("entity should be hidden without header: %d %s", without.StatusCode, without.Body)
	}
	readHeaders := adminHeaders()
	readHeaders["X-Knowledge-Changesets"] = csID
	with := doJSON(t, h, http.MethodGet, "/v1/entities/"+url.PathEscape(eid), nil, readHeaders)
	if with.StatusCode != http.StatusOK {
		t.Fatalf("entity visible with header: %d %s", with.StatusCode, with.Body)
	}

	commit := doJSON(t, h, http.MethodPost, "/v1/changesets/"+csID+"/commit", map[string]any{}, adminHeaders())
	if commit.StatusCode != http.StatusOK {
		t.Fatalf("commit open cs: %d %s", commit.StatusCode, commit.Body)
	}
	after := doJSON(t, h, http.MethodGet, "/v1/entities/"+url.PathEscape(eid), nil, adminHeaders())
	if after.StatusCode != http.StatusOK {
		t.Fatalf("entity after commit: %d %s", after.StatusCode, after.Body)
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

func TestAcceptancePackageRoot(t *testing.T) {
	h := setupTestHandler(t)
	const base = "https://example.org/root-pkg/"

	create := doJSON(t, h, http.MethodPost, "/v1/packages", map[string]any{
		"code": "root-pkg", "lifecycle": "continuous", "iriBase": base,
		"labels":       map[string]string{"en": "Root Pkg"},
		"descriptions": map[string]string{"en": "Package description on root"},
	}, nil)
	if create.StatusCode != http.StatusCreated {
		t.Fatalf("create package: %d %s", create.StatusCode, create.Body)
	}
	if !strings.Contains(create.Body, `"rootEntityId":"`+base+`"`) {
		t.Fatalf("expected rootEntityId in create response: %s", create.Body)
	}
	if !strings.Contains(create.Body, `"Package description on root"`) {
		t.Fatalf("expected descriptions in create response: %s", create.Body)
	}

	ent := doJSON(t, h, http.MethodGet, "/v1/entities/"+url.PathEscape(base), nil, nil)
	if ent.StatusCode != http.StatusOK {
		t.Fatalf("get root entity: %d %s", ent.StatusCode, ent.Body)
	}
	if !strings.Contains(ent.Body, `".package"`) || !strings.Contains(ent.Body, `"Package description on root"`) {
		t.Fatalf("root entity payload: %s", ent.Body)
	}

	// Reserved iriLocal must be rejected for normal creates.
	bad := doJSON(t, h, http.MethodPost, "/v1/entities", map[string]any{
		"packageCode": "root-pkg",
		"labels":      map[string]string{"en": "Bad"},
		"iriLocal":    ".package",
	}, nil)
	if bad.StatusCode == http.StatusCreated {
		t.Fatalf("expected reject reserved iriLocal, got %d %s", bad.StatusCode, bad.Body)
	}

	dep := doJSON(t, h, http.MethodPost, "/v1/entities/"+url.PathEscape(base)+"/deprecate", map[string]any{
		"expectedRevision": 1,
	}, nil)
	if dep.StatusCode != http.StatusConflict {
		t.Fatalf("deprecate package root: want 409, got %d %s", dep.StatusCode, dep.Body)
	}

	// After kc-base vocabulary, typing + packageCode appear.
	path, err := testdata.BundlePath("kc-base/releases/kc-base-1.1.0.bundle.json")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var bundle map[string]any
	if err := json.Unmarshal(raw, &bundle); err != nil {
		t.Fatal(err)
	}
	imp := doJSON(t, h, http.MethodPost, "/v1/releases/import", bundle, nil)
	if imp.StatusCode != http.StatusCreated {
		t.Fatalf("import kc-base: %d %s", imp.StatusCode, imp.Body)
	}
	cfg := doJSON(t, h, http.MethodGet, "/v1/admin/schema-config", nil, nil)
	if cfg.StatusCode != http.StatusOK || !strings.Contains(cfg.Body, `"instanceOfProperty":"https://knowledge-core.local/kc-base/instanceOf"`) {
		t.Fatalf("expected auto instanceOfProperty: %d %s", cfg.StatusCode, cfg.Body)
	}
	stIO := doJSON(t, h, http.MethodPost, "/v1/statements", map[string]any{
		"packageCode": "root-pkg", "subject": base,
		"property": "https://knowledge-core.local/kc-base/instanceOf",
		"value":    map[string]any{"type": "EntityReference", "entityId": "https://knowledge-core.local/kc-base/Package"},
		"upsert":   true,
	}, nil)
	if stIO.StatusCode != http.StatusCreated && stIO.StatusCode != http.StatusOK {
		t.Fatalf("instanceOf: %d %s", stIO.StatusCode, stIO.Body)
	}
	stCode := doJSON(t, h, http.MethodPost, "/v1/statements", map[string]any{
		"packageCode": "root-pkg", "subject": base,
		"property": "https://knowledge-core.local/kc-base/packageCode",
		"value":    map[string]any{"type": "String", "string": "root-pkg"},
		"upsert":   true,
	}, nil)
	if stCode.StatusCode != http.StatusCreated && stCode.StatusCode != http.StatusOK {
		t.Fatalf("packageCode: %d %s", stCode.StatusCode, stCode.Body)
	}
	codeID := parseDataID(t, stCode.Body)
	blocked := doJSON(t, h, http.MethodPost, "/v1/statements/"+url.PathEscape(codeID)+"/deprecate", map[string]any{
		"expectedRevision": 1,
	}, nil)
	if blocked.StatusCode != http.StatusConflict {
		t.Fatalf("deprecate packageCode: want 409, got %d %s", blocked.StatusCode, blocked.Body)
	}

	patchLabels := doJSON(t, h, http.MethodPatch, "/v1/packages/root-pkg", map[string]any{
		"labels": map[string]string{"en": "Root Renamed"},
	}, nil)
	if patchLabels.StatusCode != http.StatusOK {
		t.Fatalf("patch labels: %d %s", patchLabels.StatusCode, patchLabels.Body)
	}
	ent2 := doJSON(t, h, http.MethodGet, "/v1/entities/"+url.PathEscape(base), nil, nil)
	if !strings.Contains(ent2.Body, `"Root Renamed"`) {
		t.Fatalf("root labels not synced: %s", ent2.Body)
	}
}

func TestAcceptanceOpenChangeSetLifecycle(t *testing.T) {
	h := setupTestHandler(t)

	openA := doJSON(t, h, http.MethodPost, "/v1/changesets/open", map[string]any{"comment": "a"}, adminHeaders())
	if openA.StatusCode != http.StatusCreated {
		t.Fatalf("open A: %d %s", openA.StatusCode, openA.Body)
	}
	csA := parseDataID(t, openA.Body)
	hA := adminHeaders()
	hA["X-Knowledge-Changeset"] = csA

	ent := createEntity(t, h, "Shared")
	updA := doJSON(t, h, http.MethodPatch, "/v1/entities/"+url.PathEscape(ent), map[string]any{
		"labels":           map[string]string{"en": "Shared A"},
		"expectedRevision": 1,
	}, hA)
	if updA.StatusCode != http.StatusOK {
		t.Fatalf("update in A: %d %s", updA.StatusCode, updA.Body)
	}

	openB := doJSON(t, h, http.MethodPost, "/v1/changesets/open", map[string]any{"comment": "b"}, adminHeaders())
	csB := parseDataID(t, openB.Body)
	hB := adminHeaders()
	hB["X-Knowledge-Changeset"] = csB
	updB := doJSON(t, h, http.MethodPatch, "/v1/entities/"+url.PathEscape(ent), map[string]any{
		"labels":           map[string]string{"en": "Shared B"},
		"expectedRevision": 1,
	}, hB)
	if updB.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 claim conflict, got %d %s", updB.StatusCode, updB.Body)
	}

	// concurrent committed write then commit A → conflict, A stays open
	openC := doJSON(t, h, http.MethodPost, "/v1/changesets/open", map[string]any{}, adminHeaders())
	csC := parseDataID(t, openC.Body)
	hC := adminHeaders()
	hC["X-Knowledge-Changeset"] = csC
	other := createEntity(t, h, "Other")
	_ = doJSON(t, h, http.MethodPatch, "/v1/entities/"+url.PathEscape(other), map[string]any{
		"labels":           map[string]string{"en": "Other CS"},
		"expectedRevision": 1,
	}, hC)
	_ = doJSON(t, h, http.MethodPatch, "/v1/entities/"+url.PathEscape(other), map[string]any{
		"labels":           map[string]string{"en": "Other committed"},
		"expectedRevision": 1,
	}, adminHeaders())
	failCommit := doJSON(t, h, http.MethodPost, "/v1/changesets/"+csC+"/commit", map[string]any{}, adminHeaders())
	if failCommit.StatusCode != http.StatusConflict {
		t.Fatalf("expected commit conflict: %d %s", failCommit.StatusCode, failCommit.Body)
	}
	still := doJSON(t, h, http.MethodGet, "/v1/changesets/"+csC, nil, adminHeaders())
	if !strings.Contains(still.Body, `"status":"open"`) {
		t.Fatalf("cs should stay open: %s", still.Body)
	}

	cancel := doJSON(t, h, http.MethodPost, "/v1/changesets/"+csA+"/cancel", map[string]any{}, adminHeaders())
	if cancel.StatusCode != http.StatusOK {
		t.Fatalf("cancel: %d %s", cancel.StatusCode, cancel.Body)
	}
	gone := doJSON(t, h, http.MethodGet, "/v1/entities/"+url.PathEscape(ent), nil, map[string]string{
		"X-Subject": "test-admin", "X-Roles": "admin", "X-Knowledge-Changesets": csA,
	})
	// cancelled CS is not open → 400/invalid or entity without overlay (committed labels)
	if gone.StatusCode == http.StatusOK && strings.Contains(gone.Body, "Shared A") {
		t.Fatalf("cancelled overlay should not apply: %s", gone.Body)
	}

	// two open CS on different objects OK
	openD := doJSON(t, h, http.MethodPost, "/v1/changesets/open", map[string]any{}, adminHeaders())
	csD := parseDataID(t, openD.Body)
	hD := adminHeaders()
	hD["X-Knowledge-Changeset"] = csD
	createD := doJSON(t, h, http.MethodPost, "/v1/entities", map[string]any{
		"packageCode": "test",
		"labels":      map[string]string{"en": "Only D"},
	}, hD)
	if createD.StatusCode != http.StatusCreated {
		t.Fatalf("create D: %d %s", createD.StatusCode, createD.Body)
	}
	openE := doJSON(t, h, http.MethodPost, "/v1/changesets/open", map[string]any{}, adminHeaders())
	csE := parseDataID(t, openE.Body)
	hE := adminHeaders()
	hE["X-Knowledge-Changeset"] = csE
	createE := doJSON(t, h, http.MethodPost, "/v1/entities", map[string]any{
		"packageCode": "test",
		"labels":      map[string]string{"en": "Only E"},
	}, hE)
	if createE.StatusCode != http.StatusCreated {
		t.Fatalf("create E: %d %s", createE.StatusCode, createE.Body)
	}

	draftGone := doJSON(t, h, http.MethodGet, "/v1/me/changeset-draft", nil, adminHeaders())
	if draftGone.StatusCode != http.StatusNotFound {
		t.Fatalf("draft endpoint should be gone: %d", draftGone.StatusCode)
	}
}
