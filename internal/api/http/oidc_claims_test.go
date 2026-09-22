package apihttp

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestMergeRoleClaims(t *testing.T) {
	tests := []struct {
		name   string
		roles  []string
		groups []string
		want   []string
	}{
		{
			name:   "union roles then groups",
			roles:  []string{"editor"},
			groups: []string{"admin"},
			want:   []string{"editor", "admin"},
		},
		{
			name:   "groups only admin",
			roles:  nil,
			groups: []string{"admin"},
			want:   []string{"admin"},
		},
		{
			name:   "dedupe keeps roles order",
			roles:  []string{"admin", "editor"},
			groups: []string{"admin", "viewer"},
			want:   []string{"admin", "editor", "viewer"},
		},
		{
			name:   "empty and whitespace skipped",
			roles:  []string{"", "  ", "reader"},
			groups: []string{" admin "},
			want:   []string{"reader", "admin"},
		},
		{
			name:   "both empty",
			roles:  nil,
			groups: nil,
			want:   []string{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := mergeRoleClaims(tc.roles, tc.groups)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("mergeRoleClaims(%v,%v)=%v want %v", tc.roles, tc.groups, got, tc.want)
			}
		})
	}
}

func TestStringListClaimUnmarshal(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{name: "array", raw: `["admin","editor"]`, want: []string{"admin", "editor"}},
		{name: "space string", raw: `"admin editor"`, want: []string{"admin", "editor"}},
		{name: "csv string", raw: `"admin,editor"`, want: []string{"admin", "editor"}},
		{name: "null", raw: `null`, want: nil},
		{name: "empty string", raw: `""`, want: []string{}},
		{name: "invalid object", raw: `{"a":1}`, want: nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var c stringListClaim
			if err := json.Unmarshal([]byte(tc.raw), &c); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			got := []string(c)
			if tc.want == nil {
				if got != nil && len(got) != 0 {
					t.Fatalf("got %v want nil/empty", got)
				}
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestOIDCClaimsRolesAndGroupsUnion(t *testing.T) {
	raw := []byte(`{
		"sub":"user-1",
		"roles":["editor"],
		"groups":["admin","editor"]
	}`)
	var claims struct {
		Sub    string          `json:"sub"`
		Roles  stringListClaim `json:"roles"`
		Groups stringListClaim `json:"groups"`
	}
	if err := json.Unmarshal(raw, &claims); err != nil {
		t.Fatal(err)
	}
	got := mergeRoleClaims([]string(claims.Roles), []string(claims.Groups))
	want := []string{"editor", "admin"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}
