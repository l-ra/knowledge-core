package datatype

import "testing"

func TestRequireLabelEN(t *testing.T) {
	if err := RequireLabelEN(map[string]string{"cs": "x"}); err == nil {
		t.Fatal("expected error")
	}
	if err := RequireLabelEN(map[string]string{"en": "Customer"}); err != nil {
		t.Fatal(err)
	}
}

func TestValidateString(t *testing.T) {
	s := "hello"
	if err := Validate(String, Value{Type: String, String: &s}); err != nil {
		t.Fatal(err)
	}
}

func TestValidateDate(t *testing.T) {
	d := "2026-08-10"
	if err := Validate(Date, Value{Type: Date, Date: &d}); err != nil {
		t.Fatal(err)
	}
	bad := "10-08-2026"
	if err := Validate(Date, Value{Type: Date, Date: &bad}); err == nil {
		t.Fatal("expected error")
	}
}

func TestParsePublicIDs(t *testing.T) {
	n, err := ParsePublicEntityID("Q42")
	if err != nil || n != 42 {
		t.Fatalf("got %d %v", n, err)
	}
	if _, err := ParsePublicEntityID("P1"); err == nil {
		t.Fatal("expected error")
	}
}
