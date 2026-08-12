package rdf

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/l-ra/knowledge-core/internal/datatype"
)

// TurtlePrefixDecl is one @prefix / PREFIX declaration from a Turtle header.
type TurtlePrefixDecl struct {
	Prefix  string // empty = default prefix ":"
	IRIBase string
}

var (
	turtlePrefixRe      = regexp.MustCompile(`(?i)^(?:@prefix|prefix)\s+([A-Za-z_][\w.-]*:|:)\s*<([^>\s]+)>\s*\.?\s*$`)
	turtlePrefixLooseRe = regexp.MustCompile(`(?i)(?:@prefix|prefix)\s+([A-Za-z_][\w.-]*:|:)\s*<([^>\s]+)>\s*\.?`)
)

// ParseTurtlePrefixes extracts @prefix / PREFIX declarations from Turtle header text.
// Non-prefix lines are ignored. Duplicate prefixes: last wins.
func ParseTurtlePrefixes(input string) ([]TurtlePrefixDecl, []string, error) {
	lines := strings.Split(input, "\n")
	byPrefix := map[string]TurtlePrefixDecl{}
	order := []string{}
	var warnings []string

	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		m := turtlePrefixRe.FindStringSubmatch(line)
		if m == nil {
			// try loose match (multiple decls / extra junk)
			m = turtlePrefixLooseRe.FindStringSubmatch(line)
			if m == nil {
				if looksLikePrefixAttempt(line) {
					warnings = append(warnings, fmt.Sprintf("line %d: could not parse prefix declaration", i+1))
				}
				continue
			}
		}
		name := m[1]
		if name == ":" {
			name = ""
		} else {
			name = strings.TrimSuffix(name, ":")
		}
		base, err := datatype.NormalizeIRIBase(m[2])
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("line %d: invalid iriBase %q: %v", i+1, m[2], err))
			continue
		}
		if base == "" {
			warnings = append(warnings, fmt.Sprintf("line %d: empty iriBase", i+1))
			continue
		}
		if IsWellKnownVocabIRI(base) {
			warnings = append(warnings, fmt.Sprintf("line %d: skipping well-known vocab prefix %s", i+1, base))
			continue
		}
		key := name
		if _, exists := byPrefix[key]; !exists {
			order = append(order, key)
		}
		byPrefix[key] = TurtlePrefixDecl{Prefix: name, IRIBase: base}
	}

	out := make([]TurtlePrefixDecl, 0, len(order))
	for _, k := range order {
		out = append(out, byPrefix[k])
	}
	if len(out) == 0 && strings.TrimSpace(input) != "" && len(warnings) == 0 {
		return nil, nil, fmt.Errorf("no @prefix / PREFIX declarations found")
	}
	return out, warnings, nil
}

func looksLikePrefixAttempt(line string) bool {
	l := strings.ToLower(line)
	return strings.Contains(l, "@prefix") || strings.HasPrefix(l, "prefix ")
}
