package rdf_test

import (
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

func TestValidateAny(t *testing.T) {
	s := "x"
	if err := datatype.Validate(datatype.Any, datatype.Value{Type: datatype.String, String: &s}); err != nil {
		t.Fatal(err)
	}
	if err := datatype.Validate(datatype.Any, datatype.Value{Type: datatype.Any}); err == nil {
		t.Fatal("expected error")
	}
}
