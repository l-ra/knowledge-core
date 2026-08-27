package domain_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/l-ra/knowledge-core/internal/domain"
)

func TestArchimateLiteBundleUnmarshal(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	// internal/domain -> repo root
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	raw, err := os.ReadFile(filepath.Join(root, "models/archimate-lite/releases/archimate-lite-1.2.0.bundle.json"))
	if err != nil {
		t.Fatal(err)
	}
	var b domain.Bundle
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatal(err)
	}
	if b.Manifest.Package != "archimate-lite" || b.Manifest.Version != "1.2.0" {
		t.Fatalf("manifest: %+v", b.Manifest)
	}
	if b.Manifest.IRIBase != "https://knowledge-core.local/archimate-lite/" {
		t.Fatalf("iriBase: %q", b.Manifest.IRIBase)
	}
	if len(b.Manifest.ObjectIndex) == 0 || b.Manifest.ObjectIndex[0].ObjectType == "" || b.Manifest.ObjectIndex[0].ObjectPublicID == "" {
		t.Fatalf("objectIndex broken: %+v", b.Manifest.ObjectIndex)
	}
	if len(b.Classes) == 0 || len(b.Properties) == 0 || len(b.Shapes) == 0 {
		t.Fatalf("empty payload classes=%d props=%d shapes=%d", len(b.Classes), len(b.Properties), len(b.Shapes))
	}
	var relSrc *domain.BundleProperty
	for i := range b.Properties {
		if b.Properties[i].IRILocal == "relSource" {
			relSrc = &b.Properties[i]
			break
		}
	}
	if relSrc == nil || len(relSrc.Constraints.DomainClasses) == 0 || len(relSrc.Constraints.RangeClasses) == 0 {
		t.Fatalf("relSource constraints missing: %+v", relSrc)
	}
	if len(b.Statements) == 0 {
		t.Fatal("no statements")
	}
	st := b.Statements[0]
	if st.Value.Type == "" {
		t.Fatalf("statement value empty: %+v", st)
	}
}

func TestArchimateLite210BundleUnmarshal(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	raw, err := os.ReadFile(filepath.Join(root, "models/archimate-lite/releases/archimate-lite-2.2.0.bundle.json"))
	if err != nil {
		t.Fatal(err)
	}
	var b domain.Bundle
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatal(err)
	}
	if b.Manifest.Package != "archimate-lite" || b.Manifest.Version != "2.2.0" {
		t.Fatalf("manifest: %+v", b.Manifest)
	}
	rootFound := false
	for _, e := range b.Entities {
		if e.PublicID == b.Manifest.IRIBase && e.IRILocal == ".package" {
			rootFound = true
			break
		}
	}
	if !rootFound {
		t.Fatal("missing package-root entity at iriBase")
	}
	wantClass := map[string]bool{}
	for _, c := range b.Classes {
		wantClass[c.IRILocal] = true
	}
	if !wantClass["BusinessFunction"] {
		t.Fatal("missing class BusinessFunction")
	}
	wantProp := map[string]bool{}
	for _, p := range b.Properties {
		wantProp[p.IRILocal] = true
	}
	for _, name := range []string{"actorKind", "ownership", "networkKind", "networkRole", "associationKind", "flowLabel", "flowKind", "relationshipNote"} {
		if !wantProp[name] {
			t.Fatalf("missing property %s", name)
		}
	}
	wantShape := map[string]bool{}
	for _, sh := range b.Shapes {
		wantShape[sh.Code] = true
	}
	for _, code := range []string{"aml-business-function", "aml-business-actor", "aml-association", "aml-flow", "aml-communication-network", "aml-flow-network-detail"} {
		if !wantShape[code] {
			t.Fatalf("missing shape %s", code)
		}
	}
	var foundFlowDir, foundActorKind bool
	for _, e := range b.Entities {
		if e.IRILocal == "enum/flowDirection" {
			foundFlowDir = true
		}
		if e.IRILocal == "enum/actorKind" {
			foundActorKind = true
		}
	}
	if !foundFlowDir || !foundActorKind {
		t.Fatalf("enums flowDirection=%v actorKind=%v", foundFlowDir, foundActorKind)
	}
}
