package rdf_test

import (
	"strings"
	"testing"

	"github.com/l-ra/knowledge-core/internal/datatype"
	"github.com/l-ra/knowledge-core/internal/rdf"
)

func TestParseNTriples(t *testing.T) {
	in := `
# comment
<http://ex.org/a> <http://www.w3.org/2000/01/rdf-schema#label> "Alice"@en .
<http://ex.org/a> <http://ex.org/name> "Bob" .
`
	trs, err := rdf.ParseNTriples(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(trs) != 2 {
		t.Fatalf("got %d triples", len(trs))
	}
	if trs[0].Object.Lang != "en" || trs[0].Object.Value != "Alice" {
		t.Fatalf("label: %+v", trs[0].Object)
	}
}

func TestInferPropertyDatatype(t *testing.T) {
	dt := rdf.InferPropertyDatatype([]string{rdf.IRIXSDBool}, nil)
	if dt != datatype.Boolean {
		t.Fatalf("got %s", dt)
	}
	dt = rdf.InferPropertyDatatype([]string{rdf.IRILiteral}, nil)
	if dt != datatype.Any {
		t.Fatalf("literal range → Any, got %s", dt)
	}
	dt = rdf.InferPropertyDatatype(nil, []datatype.Type{datatype.String, datatype.Integer})
	if dt != datatype.Any {
		t.Fatalf("conflict → Any, got %s", dt)
	}
}

func TestSuggestPackageCode(t *testing.T) {
	got := rdf.SuggestPackageCode("https://example.org/id/")
	if got == "" || strings.Contains(got, "/") {
		t.Fatalf("bad code %q", got)
	}
}

func TestDiscoverPrefixes(t *testing.T) {
	nt := `
<https://a.example/id/x> <http://www.w3.org/2000/01/rdf-schema#label> "X" .
<https://b.example/ns/y> <http://www.w3.org/2000/01/rdf-schema#label> "Y" .
`
	trs, err := rdf.ParseNTriples(nt)
	if err != nil {
		t.Fatal(err)
	}
	cands := rdf.DiscoverPrefixes(trs)
	if len(cands) < 2 {
		t.Fatalf("expected 2 prefixes, got %+v", cands)
	}
}

func TestParseTurtlePrefixes(t *testing.T) {
	in := `
@prefix rdf: <http://www.w3.org/1999/02/22-rdf-syntax-ns#> .
@prefix ex: <https://example.org/id/> .
PREFIX person: <https://example.org/id/person/>
@prefix : <https://example.org/default/> .
`
	decls, warns, err := rdf.ParseTurtlePrefixes(in)
	if err != nil {
		t.Fatal(err)
	}
	// rdf well-known vocab skipped
	if len(decls) != 3 {
		t.Fatalf("got %+v warns=%v", decls, warns)
	}
	by := map[string]string{}
	for _, d := range decls {
		by[d.Prefix] = d.IRIBase
	}
	if by["ex"] != "https://example.org/id/" {
		t.Fatalf("ex: %q", by["ex"])
	}
	if by["person"] != "https://example.org/id/person/" {
		t.Fatalf("person: %q", by["person"])
	}
	if by[""] != "https://example.org/default/" {
		t.Fatalf("default: %q", by[""])
	}
}

func TestParseTurtlePrefixesNone(t *testing.T) {
	_, _, err := rdf.ParseTurtlePrefixes("not a prefix line")
	if err == nil {
		t.Fatal("expected error")
	}
}
