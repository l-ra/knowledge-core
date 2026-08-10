package pkgversion

import "testing"

func TestMatchesRange(t *testing.T) {
	cases := []struct {
		v, r string
		want bool
	}{
		{"3.4.0", "3.4.0", true},
		{"3.4.1", "3.4.0", false},
		{"3.5.0", "^3.4.0", true},
		{"4.0.0", "^3.4.0", false},
		{"3.3.0", "^3.4.0", false},
		{"3.4.5", "~3.4.0", true},
		{"3.5.0", "~3.4.0", false},
		{"2.5.0", ">=2.0.0 <3.0.0", true},
		{"3.0.0", ">=2.0.0 <3.0.0", false},
	}
	for _, c := range cases {
		got, err := MatchesRange(c.v, c.r)
		if err != nil {
			t.Fatalf("%s %s: %v", c.v, c.r, err)
		}
		if got != c.want {
			t.Fatalf("%s satisfies %s: got %v want %v", c.v, c.r, got, c.want)
		}
	}
}
