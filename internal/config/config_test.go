package config

import "testing"

func TestParseOIDCScopes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want string
	}{
		{"", DefaultOIDCScopes},
		{"  ", DefaultOIDCScopes},
		{"openid profile email", "openid profile email"},
		{"openid,profile,email,groups", "openid profile email groups"},
		{"openid, profile,  email", "openid profile email"},
	}
	for _, tc := range cases {
		got := parseOIDCScopes(tc.in)
		if got != tc.want {
			t.Fatalf("parseOIDCScopes(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}
