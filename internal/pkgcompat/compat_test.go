package pkgcompat_test

import (
	"testing"

	"github.com/l-ra/knowledge-core/internal/datatype"
	"github.com/l-ra/knowledge-core/internal/domain"
	"github.com/l-ra/knowledge-core/internal/pkgcompat"
)

func intp(n int) *int { return &n }

func baseSnap() pkgcompat.Snapshot {
	return pkgcompat.Snapshot{
		Package: "demo",
		Version: "1.0.0",
		Classes: map[string]domain.BundleClass{
			"C1": {PublicID: "C1", IRILocal: "Thing", Status: "active", Labels: map[string]string{"en": "Thing"}},
		},
		Properties: map[string]domain.BundleProperty{
			"P1": {
				PublicID: "P1", IRILocal: "name", Datatype: datatype.String, Status: "active",
				Labels:      map[string]string{"en": "name"},
				Constraints: domain.PropertyConstraints{MinCount: intp(0), MaxCount: intp(1)},
			},
		},
		Shapes: map[string]domain.BundleShape{
			"sh1": {
				Code: "sh1", ClassID: "C1",
				Document: domain.ShapeDocument{RequiredProperties: []string{"P1"}, Closed: false},
			},
		},
		Entities:   map[string]domain.BundleEntity{},
		Statements: map[string]domain.BundleStatement{},
	}
}

func TestDiff_AdditiveProperty(t *testing.T) {
	old := baseSnap()
	neu := baseSnap()
	neu.Version = "1.1.0"
	neu.Properties["P2"] = domain.BundleProperty{
		PublicID: "P2", IRILocal: "title", Datatype: datatype.String, Status: "active",
		Labels: map[string]string{"en": "title"},
	}
	f := pkgcompat.Diff(old, neu)
	if pkgcompat.HasBreaking(f) {
		t.Fatalf("unexpected breaking: %v", f)
	}
	found := false
	for _, x := range f {
		if x.ReasonCode == pkgcompat.ReasonObjectAdded && x.ObjectPublicID == "P2" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected object_added for P2: %v", f)
	}
}

func TestDiff_DatatypeChangeBreaking(t *testing.T) {
	old := baseSnap()
	neu := baseSnap()
	p := neu.Properties["P1"]
	p.Datatype = datatype.Integer
	neu.Properties["P1"] = p
	f := pkgcompat.Diff(old, neu)
	if !pkgcompat.HasBreaking(f) {
		t.Fatalf("expected breaking, got %v", f)
	}
}

func TestDiff_MinCountTightenBreaking(t *testing.T) {
	old := baseSnap()
	neu := baseSnap()
	p := neu.Properties["P1"]
	p.Constraints.MinCount = intp(1)
	neu.Properties["P1"] = p
	f := pkgcompat.Diff(old, neu)
	if !pkgcompat.HasBreaking(f) {
		t.Fatalf("expected breaking minCount: %v", f)
	}
}

func TestDiff_MinCountLoosenAdditive(t *testing.T) {
	old := baseSnap()
	p := old.Properties["P1"]
	p.Constraints.MinCount = intp(1)
	old.Properties["P1"] = p
	neu := baseSnap()
	f := pkgcompat.Diff(old, neu)
	if pkgcompat.HasBreaking(f) {
		t.Fatalf("unexpected breaking: %v", f)
	}
}

func TestDiff_PropertyRemovedBreaking(t *testing.T) {
	old := baseSnap()
	neu := baseSnap()
	delete(neu.Properties, "P1")
	f := pkgcompat.Diff(old, neu)
	if !pkgcompat.HasBreaking(f) {
		t.Fatalf("expected breaking removal: %v", f)
	}
}

func TestDiff_ShapeRequiredAddedBreaking(t *testing.T) {
	old := baseSnap()
	neu := baseSnap()
	neu.Properties["P2"] = domain.BundleProperty{PublicID: "P2", IRILocal: "x", Datatype: datatype.String, Status: "active", Labels: map[string]string{"en": "x"}}
	sh := neu.Shapes["sh1"]
	sh.Document.RequiredProperties = []string{"P1", "P2"}
	neu.Shapes["sh1"] = sh
	f := pkgcompat.Diff(old, neu)
	if !pkgcompat.HasBreaking(f) {
		t.Fatalf("expected shape tighten: %v", f)
	}
}

func TestDiff_ShapeRequiredRemovedAdditive(t *testing.T) {
	old := baseSnap()
	neu := baseSnap()
	sh := neu.Shapes["sh1"]
	sh.Document.RequiredProperties = nil
	neu.Shapes["sh1"] = sh
	f := pkgcompat.Diff(old, neu)
	if pkgcompat.HasBreaking(f) {
		t.Fatalf("unexpected breaking: %v", f)
	}
}

func TestDiff_DomainNarrowBreaking(t *testing.T) {
	old := baseSnap()
	p := old.Properties["P1"]
	p.Constraints.DomainClasses = []string{"C1", "C2"}
	old.Properties["P1"] = p
	neu := baseSnap()
	p2 := neu.Properties["P1"]
	p2.Constraints.DomainClasses = []string{"C1"}
	neu.Properties["P1"] = p2
	f := pkgcompat.Diff(old, neu)
	if !pkgcompat.HasBreaking(f) {
		t.Fatalf("expected domain narrow breaking: %v", f)
	}
}

func TestDiff_LabelsMetadata(t *testing.T) {
	old := baseSnap()
	neu := baseSnap()
	c := neu.Classes["C1"]
	c.Labels = map[string]string{"en": "Thing2"}
	neu.Classes["C1"] = c
	f := pkgcompat.Diff(old, neu)
	if pkgcompat.HasBreaking(f) {
		t.Fatalf("labels should be metadata: %v", f)
	}
}

func strp(s string) *string { return &s }

func TestDiff_CatalogVersionStatementMetadata(t *testing.T) {
	old := baseSnap()
	old.Statements["S1"] = domain.BundleStatement{
		PublicID: "S1", Subject: "E1", Property: "https://example/catalogVersion",
		Status: "active", Value: datatype.Value{Type: datatype.String, String: strp("1.0.0")},
	}
	old.Properties["https://example/catalogVersion"] = domain.BundleProperty{
		PublicID: "https://example/catalogVersion", IRILocal: "catalogVersion", Datatype: datatype.String, Status: "active",
		Labels: map[string]string{"en": "v"},
	}
	neu := baseSnap()
	neu.Properties["https://example/catalogVersion"] = old.Properties["https://example/catalogVersion"]
	neu.Statements["S1"] = domain.BundleStatement{
		PublicID: "S1", Subject: "E1", Property: "https://example/catalogVersion",
		Status: "active", Value: datatype.Value{Type: datatype.String, String: strp("1.1.0")},
	}
	f := pkgcompat.Diff(old, neu)
	if pkgcompat.HasBreaking(f) {
		t.Fatalf("catalogVersion change should be metadata: %v", f)
	}
}

func TestDiff_AllowedValueRemovedBreaking(t *testing.T) {
	old := baseSnap()
	old.Statements["S-enum"] = domain.BundleStatement{
		PublicID: "S-enum", Subject: "E-enum", Property: "https://example/allowedValue",
		Status: "active", Value: datatype.Value{Type: datatype.String, String: strp("a")},
	}
	neu := baseSnap()
	f := pkgcompat.Diff(old, neu)
	if !pkgcompat.HasBreaking(f) {
		t.Fatalf("removed allowedValue statement must break: %v", f)
	}
}

func TestDiff_SubClassChangeBreaking(t *testing.T) {
	old := baseSnap()
	c := old.Classes["C1"]
	c.SubClassOf = "C0"
	old.Classes["C1"] = c
	neu := baseSnap()
	c2 := neu.Classes["C1"]
	c2.SubClassOf = "C9"
	neu.Classes["C1"] = c2
	f := pkgcompat.Diff(old, neu)
	if !pkgcompat.HasBreaking(f) {
		t.Fatalf("subclass change must break: %v", f)
	}
}

func TestBreakingError(t *testing.T) {
	err := pkgcompat.NewBreakingError("demo@1.1", []pkgcompat.Finding{
		{Kind: pkgcompat.KindBreaking, ReasonCode: pkgcompat.ReasonDatatypeChanged, ObjectType: "property", ObjectPublicID: "P1"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if pkgcompat.NewBreakingError("", nil) != nil {
		t.Fatal("no breaking findings → nil")
	}
}

func TestFilterByPackage_DropsDependencyObjects(t *testing.T) {
	s := pkgcompat.Snapshot{
		Package: "archimate-lite",
		Version: "2.0.0",
		Classes: map[string]domain.BundleClass{
			"aml":  {PublicID: "aml", PackageCode: "archimate-lite", IRILocal: "BusinessActor"},
			"base": {PublicID: "base", PackageCode: "kc-base", IRILocal: "StringEnum"},
		},
		Properties: map[string]domain.BundleProperty{
			"p1": {PublicID: "p1", PackageCode: "archimate-lite", IRILocal: "actorKind"},
			"p2": {PublicID: "p2", PackageCode: "kc-base", IRILocal: "instanceOf"},
		},
		Shapes: map[string]domain.BundleShape{
			"aml-element": {Code: "aml-element", PackageCode: "archimate-lite"},
			"string-enum": {Code: "string-enum", PackageCode: "kc-base"},
		},
		Entities:   map[string]domain.BundleEntity{},
		Statements: map[string]domain.BundleStatement{},
	}
	got := pkgcompat.FilterByPackage(s, "archimate-lite")
	if _, ok := got.Classes["base"]; ok {
		t.Fatal("kc-base class should be dropped")
	}
	if _, ok := got.Properties["p2"]; ok {
		t.Fatal("kc-base property should be dropped")
	}
	if _, ok := got.Shapes["string-enum"]; ok {
		t.Fatal("kc-base shape should be dropped")
	}
	if _, ok := got.Classes["aml"]; !ok {
		t.Fatal("archimate-lite class should remain")
	}
}
