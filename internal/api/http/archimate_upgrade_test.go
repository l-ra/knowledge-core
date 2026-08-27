package apihttp_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Seed archimate-lite 1.2.0 is an additive upgrade of 1.1.0. Import must succeed
// after 1.1.0 is installed (BC gate compares against release export).
func TestAcceptanceArchimateLiteUpgrade110to120(t *testing.T) {
	h := setupTestHandler(t)
	root := filepath.Join("..", "..", "..")
	load := func(name string) map[string]any {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(root, "models/archimate-lite/releases", name))
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}

	imp1 := doJSON(t, h, http.MethodPost, "/v1/releases/import", load("archimate-lite-1.1.0.bundle.json"), nil)
	if imp1.StatusCode != http.StatusCreated {
		t.Fatalf("import 1.1.0: %d %s", imp1.StatusCode, imp1.Body)
	}

	exp := doJSON(t, h, http.MethodGet, "/v1/packages/archimate-lite/releases/1.1.0/bundle", nil, nil)
	if exp.StatusCode != http.StatusOK {
		t.Fatalf("export 1.1.0: %d %s", exp.StatusCode, exp.Body)
	}
	var exported map[string]any
	if err := json.Unmarshal([]byte(exp.Body), &exported); err != nil {
		t.Fatal(err)
	}
	wantID := "https://knowledge-core.local/archimate-lite/statement/allowed/Access/ApplicationComponent/DataObject/instanceOf"
	var gotEntityID string
	for _, raw := range exported["statements"].([]any) {
		st := raw.(map[string]any)
		if st["id"] != wantID {
			continue
		}
		val := st["value"].(map[string]any)
		gotEntityID, _ = val["entityId"].(string)
		break
	}
	if gotEntityID != "https://knowledge-core.local/archimate-lite/AllowedRelationship" {
		t.Fatalf("export EntityReference must use public id, got %q", gotEntityID)
	}

	imp2 := doJSON(t, h, http.MethodPost, "/v1/releases/import", load("archimate-lite-1.2.0.bundle.json"), nil)
	if imp2.StatusCode != http.StatusCreated {
		t.Fatalf("import 1.2.0: %d %s", imp2.StatusCode, imp2.Body)
	}

	pkgs := doJSON(t, h, http.MethodGet, "/v1/packages", nil, nil)
	if pkgs.StatusCode != http.StatusOK {
		t.Fatalf("list packages: %d %s", pkgs.StatusCode, pkgs.Body)
	}
	if !strings.Contains(pkgs.Body, `"latestReleaseVersion":"1.2.0"`) {
		t.Fatalf("expected latestReleaseVersion 1.2.0 in list, got %s", pkgs.Body)
	}
	if strings.Contains(pkgs.Body, `"modifiedAfterRelease":true`) {
		t.Fatalf("fresh import should not be modifiedAfterRelease: %s", pkgs.Body)
	}
}

func TestAcceptanceArchimateLiteUpgrade200to210(t *testing.T) {
	h := setupTestHandler(t)
	root := filepath.Join("..", "..", "..")
	load := func(rel string) map[string]any {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}

	impBase := doJSON(t, h, http.MethodPost, "/v1/releases/import", load("models/kc-base/releases/kc-base-1.0.0.bundle.json"), nil)
	if impBase.StatusCode != http.StatusCreated {
		t.Fatalf("import kc-base 1.0.0: %d %s", impBase.StatusCode, impBase.Body)
	}
	cfg := doJSON(t, h, http.MethodPut, "/v1/admin/schema-config", map[string]any{
		"instanceOfProperty": "https://knowledge-core.local/kc-base/instanceOf",
	}, nil)
	if cfg.StatusCode != http.StatusOK {
		t.Fatalf("schema-config: %d %s", cfg.StatusCode, cfg.Body)
	}

	imp20 := doJSON(t, h, http.MethodPost, "/v1/releases/import", load("models/archimate-lite/releases/archimate-lite-2.0.0.bundle.json"), nil)
	if imp20.StatusCode != http.StatusCreated {
		t.Fatalf("import 2.0.0: %d %s", imp20.StatusCode, imp20.Body)
	}

	imp21 := doJSON(t, h, http.MethodPost, "/v1/releases/import", load("models/archimate-lite/releases/archimate-lite-2.1.0.bundle.json"), nil)
	if imp21.StatusCode != http.StatusCreated {
		t.Fatalf("import 2.1.0: %d %s", imp21.StatusCode, imp21.Body)
	}

	pkgs := doJSON(t, h, http.MethodGet, "/v1/packages", nil, nil)
	if pkgs.StatusCode != http.StatusOK {
		t.Fatalf("list packages: %d %s", pkgs.StatusCode, pkgs.Body)
	}
	if !strings.Contains(pkgs.Body, `"latestReleaseVersion":"2.1.0"`) {
		t.Fatalf("expected latestReleaseVersion 2.1.0 in list, got %s", pkgs.Body)
	}

	impBase11 := doJSON(t, h, http.MethodPost, "/v1/releases/import", load("models/kc-base/releases/kc-base-1.1.0.bundle.json"), nil)
	if impBase11.StatusCode != http.StatusCreated {
		t.Fatalf("import kc-base 1.1.0: %d %s", impBase11.StatusCode, impBase11.Body)
	}
	imp22 := doJSON(t, h, http.MethodPost, "/v1/releases/import", load("models/archimate-lite/releases/archimate-lite-2.2.0.bundle.json"), nil)
	if imp22.StatusCode != http.StatusCreated {
		t.Fatalf("import 2.2.0: %d %s", imp22.StatusCode, imp22.Body)
	}
	pkgs22 := doJSON(t, h, http.MethodGet, "/v1/packages", nil, nil)
	if pkgs22.StatusCode != http.StatusOK || !strings.Contains(pkgs22.Body, `"latestReleaseVersion":"2.2.0"`) {
		t.Fatalf("expected latestReleaseVersion 2.2.0 in list, got %s", pkgs22.Body)
	}
	amlPkg := doJSON(t, h, http.MethodGet, "/v1/packages/archimate-lite", nil, nil)
	if amlPkg.StatusCode != http.StatusOK || !strings.Contains(amlPkg.Body, `"rootEntityId":"https://knowledge-core.local/archimate-lite/"`) {
		t.Fatalf("archimate-lite package-root: %d %s", amlPkg.StatusCode, amlPkg.Body)
	}
	rootEnt := doJSON(t, h, http.MethodGet, "/v1/entities/"+url.PathEscape("https://knowledge-core.local/archimate-lite/"), nil, nil)
	if rootEnt.StatusCode != http.StatusOK || !strings.Contains(rootEnt.Body, `".package"`) {
		t.Fatalf("package-root entity: %d %s", rootEnt.StatusCode, rootEnt.Body)
	}

	const aml = "https://knowledge-core.local/archimate-lite/"
	const kcBase = "https://knowledge-core.local/kc-base/"
	bf := doJSON(t, h, http.MethodGet, "/v1/classes/"+url.PathEscape(aml+"BusinessFunction"), nil, nil)
	if bf.StatusCode != http.StatusOK {
		t.Fatalf("BusinessFunction class: %d %s", bf.StatusCode, bf.Body)
	}
	layer := doJSON(t, h, http.MethodGet, entityPath(aml+"BusinessFunction", "/statements")+"?property="+url.QueryEscape(aml+"archiLayer"), nil, nil)
	if layer.StatusCode != http.StatusOK || !strings.Contains(layer.Body, `"business"`) {
		t.Fatalf("BusinessFunction archiLayer: %d %s", layer.StatusCode, layer.Body)
	}

	for _, code := range []string{"aml-business-actor", "aml-flow", "aml-association", "aml-communication-network", "aml-flow-network-detail", "aml-business-function"} {
		sh := doJSON(t, h, http.MethodGet, "/v1/shapes/"+url.PathEscape(code)+"?package=archimate-lite", nil, nil)
		if sh.StatusCode != http.StatusOK {
			t.Fatalf("shape %s: %d %s", code, sh.StatusCode, sh.Body)
		}
	}
	for _, local := range []string{"enum/actorKind", "enum/flowDirection", "enum/dependencyType", "enum/associationKind"} {
		en := doJSON(t, h, http.MethodGet, "/v1/entities?package=archimate-lite&iriLocal="+url.QueryEscape(local), nil, nil)
		if en.StatusCode != http.StatusOK || !strings.Contains(en.Body, local) {
			t.Fatalf("enum %s: %d %s", local, en.StatusCode, en.Body)
		}
	}

	pkg := doJSON(t, h, http.MethodPost, "/v1/packages", map[string]any{
		"code": "aml-demo", "lifecycle": "continuous",
		"iriBase": "https://knowledge-core.local/archimate-lite-demo/",
		"labels":  map[string]string{"en": "aml-demo"},
		"dependencies": []map[string]string{{
			"dependsOnCode": "archimate-lite", "versionRange": "^2.2.0",
		}},
	}, nil)
	if pkg.StatusCode != http.StatusCreated {
		t.Fatalf("create aml-demo: %d %s", pkg.StatusCode, pkg.Body)
	}

	createAmlEntity := func(local, label, class string) string {
		t.Helper()
		res := doJSON(t, h, http.MethodPost, "/v1/entities", map[string]any{
			"packageCode": "aml-demo",
			"iriLocal":    local,
			"labels":      map[string]string{"en": label},
		}, nil)
		if res.StatusCode != http.StatusCreated {
			t.Fatalf("create %s: %d %s", local, res.StatusCode, res.Body)
		}
		id := parseDataID(t, res.Body)
		st := doJSON(t, h, http.MethodPost, "/v1/statements", map[string]any{
			"packageCode": "aml-demo", "subject": id,
			"property": kcBase + "instanceOf",
			"value":    map[string]any{"type": "EntityReference", "entityId": aml + class},
			"upsert":   true,
		}, nil)
		if st.StatusCode != http.StatusCreated && st.StatusCode != http.StatusOK {
			t.Fatalf("instanceOf %s: %d %s", local, st.StatusCode, st.Body)
		}
		return id
	}
	putString := func(subject, prop, val string) {
		t.Helper()
		st := doJSON(t, h, http.MethodPost, "/v1/statements", map[string]any{
			"packageCode": "aml-demo", "subject": subject,
			"property": aml + prop,
			"value":    map[string]any{"type": "String", "string": val},
			"upsert":   true,
		}, nil)
		if st.StatusCode != http.StatusCreated && st.StatusCode != http.StatusOK {
			t.Fatalf("stmt %s %s: %d %s", subject, prop, st.StatusCode, st.Body)
		}
	}
	putRef := func(subject, prop, target string) {
		t.Helper()
		st := doJSON(t, h, http.MethodPost, "/v1/statements", map[string]any{
			"packageCode": "aml-demo", "subject": subject,
			"property": aml + prop,
			"value":    map[string]any{"type": "EntityReference", "entityId": target},
			"upsert":   true,
		}, nil)
		if st.StatusCode != http.StatusCreated && st.StatusCode != http.StatusOK {
			t.Fatalf("ref %s %s: %d %s", subject, prop, st.StatusCode, st.Body)
		}
	}
	putInt := func(subject, prop string, n int) {
		t.Helper()
		st := doJSON(t, h, http.MethodPost, "/v1/statements", map[string]any{
			"packageCode": "aml-demo", "subject": subject,
			"property": aml + prop,
			"value":    map[string]any{"type": "Integer", "int64": n},
			"upsert":   true,
		}, nil)
		if st.StatusCode != http.StatusCreated && st.StatusCode != http.StatusOK {
			t.Fatalf("int %s %s: %d %s", subject, prop, st.StatusCode, st.Body)
		}
	}
	validationErrors := func(id string) int {
		t.Helper()
		val := doJSON(t, h, http.MethodGet, entityPath(id, "/validation"), nil, nil)
		if val.StatusCode != http.StatusOK {
			t.Fatalf("validation %s: %d %s", id, val.StatusCode, val.Body)
		}
		var body struct {
			Summary struct {
				Errors int `json:"errors"`
			} `json:"summary"`
			Findings []struct {
				Code      string `json:"code"`
				Severity  string `json:"severity"`
				ShapeCode string `json:"shapeCode"`
			} `json:"findings"`
		}
		if err := json.Unmarshal([]byte(val.Body), &body); err != nil {
			t.Fatal(err)
		}
		return body.Summary.Errors
	}
	hasShapeError := func(id, shape string) bool {
		t.Helper()
		val := doJSON(t, h, http.MethodGet, entityPath(id, "/validation"), nil, nil)
		var body struct {
			Findings []struct {
				Code      string `json:"code"`
				Severity  string `json:"severity"`
				ShapeCode string `json:"shapeCode"`
			} `json:"findings"`
		}
		_ = json.Unmarshal([]byte(val.Body), &body)
		for _, f := range body.Findings {
			if f.ShapeCode == shape && f.Code == "shape_required_missing" && f.Severity == "error" {
				return true
			}
		}
		return false
	}

	incompleteActor := createAmlEntity("incomplete-actor", "Missing kind", "BusinessActor")
	if !hasShapeError(incompleteActor, "aml-business-actor") {
		t.Fatalf("expected aml-business-actor error on actor without actorKind")
	}

	incompleteFlow := createAmlEntity("incomplete-flow", "Missing label", "Flow")
	src := createAmlEntity("tmp-src", "Tmp src", "ApplicationComponent")
	tgt := createAmlEntity("tmp-tgt", "Tmp tgt", "ApplicationComponent")
	putRef(incompleteFlow, "relSource", src)
	putRef(incompleteFlow, "relTarget", tgt)
	if !hasShapeError(incompleteFlow, "aml-flow") {
		t.Fatalf("expected aml-flow error on Flow without flowLabel")
	}

	dept := createAmlEntity("compliance-dept", "Odbor Compliance", "BusinessActor")
	putString(dept, "actorKind", "department")
	putString(dept, "modelingDepth", "catalog")
	petr := createAmlEntity("petr-novak", "Petr Novák", "BusinessActor")
	putString(petr, "actorKind", "person")
	putString(petr, "modelingDepth", "catalog")
	jiri := createAmlEntity("jiri-kreuzman", "Jiri Kreuzman", "BusinessActor")
	putString(jiri, "actorKind", "person")
	putString(jiri, "modelingDepth", "catalog")
	dpo := createAmlEntity("dpo", "DPO", "BusinessRole")
	putString(dpo, "modelingDepth", "catalog")
	fn := createAmlEntity("data-protection", "Data Protection", "BusinessFunction")
	putString(fn, "modelingDepth", "catalog")

	comp := createAmlEntity("rel-dept-petr", "dept contains petr", "Composition")
	putRef(comp, "relSource", dept)
	putRef(comp, "relTarget", petr)
	asgRole := createAmlEntity("rel-petr-dpo", "petr to dpo", "Assignment")
	putRef(asgRole, "relSource", petr)
	putRef(asgRole, "relTarget", dpo)
	asgFn := createAmlEntity("rel-dept-function", "dept to function", "Assignment")
	putRef(asgFn, "relSource", dept)
	putRef(asgFn, "relTarget", fn)
	reports := createAmlEntity("rel-reports-to", "reportsTo", "Association")
	putRef(reports, "relSource", petr)
	putRef(reports, "relTarget", jiri)
	putString(reports, "associationKind", "reportsTo")

	crm := createAmlEntity("crm-backend", "CRM Backend", "ApplicationComponent")
	putString(crm, "modelingDepth", "application_detail")
	putString(crm, "ownership", "internal")
	pay := createAmlEntity("payment-gateway", "Payment Gateway", "ApplicationComponent")
	putString(pay, "modelingDepth", "application_detail")
	putString(pay, "ownership", "external")
	data := createAmlEntity("customer-data", "CustomerData", "DataObject")
	putString(data, "modelingDepth", "application_detail")
	orders := createAmlEntity("rel-orders-flow", "Customer orders", "Flow")
	putRef(orders, "relSource", crm)
	putRef(orders, "relTarget", pay)
	putString(orders, "flowLabel", "Customer orders")
	putString(orders, "flowKind", "data")
	putString(orders, "protocol", "HTTPS")
	access := createAmlEntity("rel-customer-access", "access CustomerData", "Access")
	putRef(access, "relSource", crm)
	putRef(access, "relTarget", data)
	putString(access, "accessMode", "readWrite")

	appNet := createAmlEntity("app-net", "APP_NET", "CommunicationNetwork")
	putString(appNet, "modelingDepth", "network_detail")
	putString(appNet, "networkKind", "vlan")
	putString(appNet, "networkRole", "production")
	putString(appNet, "cidr", "10.1.0.0/16")
	dcWAN := createAmlEntity("dc-wan", "DC-WAN", "CommunicationNetwork")
	putString(dcWAN, "modelingDepth", "network_detail")
	putString(dcWAN, "networkKind", "wan")
	putString(dcWAN, "networkRole", "wan")
	cluster := createAmlEntity("prod-cluster", "prod-cluster", "Node")
	putString(cluster, "modelingDepth", "infrastructure_detail")
	pathEnt := createAmlEntity("wan-path", "wan-path", "Path")
	putString(pathEnt, "modelingDepth", "network_detail")
	member := createAmlEntity("rel-cluster-net", "memberOf APP_NET", "Association")
	putRef(member, "relSource", cluster)
	putRef(member, "relTarget", appNet)
	putString(member, "associationKind", "memberOf")
	wanFlow := createAmlEntity("rel-wan-flow", "Inter-DC replication", "Flow")
	putRef(wanFlow, "relSource", dcWAN)
	putRef(wanFlow, "relTarget", appNet)
	putString(wanFlow, "flowLabel", "Inter-DC replication")
	putString(wanFlow, "flowKind", "data")
	putString(wanFlow, "protocol", "TCP")
	putInt(wanFlow, "port", 5432)
	spans := createAmlEntity("rel-path-spans", "wan-path spans DC-WAN", "Association")
	putRef(spans, "relSource", pathEnt)
	putRef(spans, "relTarget", dcWAN)
	putString(spans, "associationKind", "spans")

	for _, id := range []string{dept, petr, jiri, dpo, fn, comp, asgRole, asgFn, reports, crm, pay, data, orders, access, appNet, dcWAN, cluster, pathEnt, member, wanFlow, spans} {
		if n := validationErrors(id); n != 0 {
			val := doJSON(t, h, http.MethodGet, entityPath(id, "/validation"), nil, nil)
			t.Fatalf("demo entity %s has %d validation errors: %s", id, n, val.Body)
		}
	}
}
