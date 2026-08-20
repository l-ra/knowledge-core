package apihttp_test

import (
	"encoding/json"
	"net/http"
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
