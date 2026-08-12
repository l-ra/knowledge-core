package datatype_test

import (
	"testing"

	"github.com/l-ra/knowledge-core/internal/datatype"
)

func TestNormalizeIRIBase(t *testing.T) {
	got, err := datatype.NormalizeIRIBase("https://example.org/id")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://example.org/id/" {
		t.Fatalf("got %q", got)
	}
	if _, err := datatype.NormalizeIRIBase("ftp://x/"); err == nil {
		t.Fatal("expected error for ftp")
	}
}

func TestNormalizeIRILocal(t *testing.T) {
	got, err := datatype.NormalizeIRILocal("/person/42")
	if err != nil {
		t.Fatal(err)
	}
	if got != "person/42" {
		t.Fatalf("got %q", got)
	}
	if _, err := datatype.NormalizeIRILocal("https://example.org/x"); err == nil {
		t.Fatal("expected error for absolute")
	}
}

func TestResolveIRI(t *testing.T) {
	iri := datatype.ResolveIRI("https://ex.org/", "person/1", "Q1", "https://knowledge-core.local/entity/")
	if iri != "https://ex.org/person/1" {
		t.Fatalf("got %q", iri)
	}
	iri = datatype.ResolveIRI("", "", "Q9", "https://knowledge-core.local/entity/")
	if iri != "https://knowledge-core.local/entity/Q9" {
		t.Fatalf("fallback got %q", iri)
	}
}
