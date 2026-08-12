package rdf

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"unicode"
)

// NamespaceOf returns the IRI base ending with / or # (last path or fragment separator).
func NamespaceOf(iri string) string {
	iri = strings.TrimSpace(iri)
	if iri == "" {
		return ""
	}
	i := strings.LastIndexAny(iri, "/#")
	if i < 0 {
		return iri
	}
	return iri[:i+1]
}

// IsWellKnownVocabIRI reports RDF/RDFS/OWL/XSD (and KC ontology) IRIs to ignore as package candidates.
func IsWellKnownVocabIRI(iri string) bool {
	prefixes := []string{
		"http://www.w3.org/1999/02/22-rdf-syntax-ns#",
		"http://www.w3.org/2000/01/rdf-schema#",
		"http://www.w3.org/2002/07/owl#",
		"http://www.w3.org/2001/XMLSchema#",
		"https://knowledge-core.local/ontology/",
		"https://knowledge-core.local/entity/",
		"https://knowledge-core.local/property/",
		"https://knowledge-core.local/statement/",
	}
	for _, p := range prefixes {
		if strings.HasPrefix(iri, p) {
			return true
		}
	}
	return false
}

// SuggestPackageCode derives a package code from an IRI base.
func SuggestPackageCode(iriBase string) string {
	u, err := url.Parse(strings.TrimSpace(iriBase))
	if err != nil || u.Host == "" {
		s := strings.TrimRight(iriBase, "/#")
		s = strings.Map(func(r rune) rune {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				return unicode.ToLower(r)
			}
			return '-'
		}, s)
		return trimDashes(s)
	}
	host := strings.TrimPrefix(u.Host, "www.")
	parts := []string{}
	for _, p := range strings.Split(host, ".") {
		p = strings.TrimSpace(p)
		if p != "" && p != "www" {
			parts = append(parts, p)
		}
	}
	path := strings.Trim(u.Path, "/")
	if path != "" {
		for _, p := range strings.Split(path, "/") {
			p = strings.TrimSpace(p)
			if p != "" {
				parts = append(parts, p)
			}
		}
	}
	if u.Fragment != "" {
		parts = append(parts, u.Fragment)
	}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.Map(func(r rune) rune {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				return unicode.ToLower(r)
			}
			return '-'
		}, p)
		p = trimDashes(p)
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return "imported"
	}
	code := strings.Join(out, "-")
	if len(code) > 64 {
		code = code[:64]
		code = strings.TrimRight(code, "-")
	}
	return code
}

func trimDashes(s string) string {
	for strings.HasPrefix(s, "-") {
		s = s[1:]
	}
	for strings.HasSuffix(s, "-") {
		s = s[:len(s)-1]
	}
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return s
}

// PrefixCandidate is a discovered IRI namespace in an RDF graph.
type PrefixCandidate struct {
	IRIBase      string
	IRICount     int
	TripleCount  int
	SampleIRIs   []string
	SuggestedCode string
}

// DiscoverPrefixes finds IRI bases from non-vocab IRIs in triples.
func DiscoverPrefixes(triples []Triple) []PrefixCandidate {
	iriCount := map[string]int{}
	samples := map[string][]string{}
	allIRIs := map[string]struct{}{}

	add := func(iri string) {
		if iri == "" || IsWellKnownVocabIRI(iri) {
			return
		}
		allIRIs[iri] = struct{}{}
		base := NamespaceOf(iri)
		if base == "" || IsWellKnownVocabIRI(base) {
			return
		}
		iriCount[base]++
		if len(samples[base]) < 5 {
			for _, s := range samples[base] {
				if s == iri {
					return
				}
			}
			samples[base] = append(samples[base], iri)
		}
	}

	for _, tr := range triples {
		if tr.Subject.Kind == TermIRI {
			add(tr.Subject.Value)
		}
		if tr.Predicate.Kind == TermIRI {
			add(tr.Predicate.Value)
		}
		if tr.Object.Kind == TermIRI {
			add(tr.Object.Value)
		}
	}

	// Collect leaf bases, then add shared parent namespaces (≥2 children).
	bases := make([]string, 0, len(iriCount))
	for b := range iriCount {
		bases = append(bases, b)
	}
	parentHits := map[string]int{}
	for _, b := range bases {
		for p := parentNamespace(b); p != "" && p != b; p = parentNamespace(p) {
			parentHits[p]++
		}
	}
	for p, n := range parentHits {
		if n < 2 {
			continue
		}
		if _, ok := iriCount[p]; !ok {
			cnt := 0
			for iri := range allIRIs {
				if strings.HasPrefix(iri, p) {
					cnt++
				}
			}
			iriCount[p] = cnt
		}
	}
	kept := make([]string, 0, len(iriCount))
	for b := range iriCount {
		kept = append(kept, b)
	}

	// Triple counts for display: longest matching base among kept
	tripleCount := map[string]int{}
	for _, tr := range triples {
		if tr.Subject.Kind != TermIRI {
			continue
		}
		base := LongestMatchingBase(tr.Subject.Value, kept)
		if base != "" {
			tripleCount[base]++
		}
	}

	out := make([]PrefixCandidate, 0, len(kept))
	for _, b := range kept {
		out = append(out, PrefixCandidate{
			IRIBase:       b,
			IRICount:      iriCount[b],
			TripleCount:   tripleCount[b],
			SampleIRIs:    samples[b],
			SuggestedCode: SuggestPackageCode(b),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IRICount != out[j].IRICount {
			return out[i].IRICount > out[j].IRICount
		}
		if out[i].TripleCount != out[j].TripleCount {
			return out[i].TripleCount > out[j].TripleCount
		}
		return out[i].IRIBase < out[j].IRIBase
	})
	return out
}

func parentNamespace(base string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		return ""
	}
	trim := strings.TrimRight(base, "/#")
	if trim == "" {
		return ""
	}
	i := strings.LastIndexAny(trim, "/#")
	if i < 0 {
		return ""
	}
	return trim[:i+1]
}

// LongestMatchingBase returns the longest base in bases that is a prefix of iri.
func LongestMatchingBase(iri string, bases []string) string {
	best := ""
	for _, b := range bases {
		if b != "" && strings.HasPrefix(iri, b) && len(b) > len(best) {
			best = b
		}
	}
	return best
}

// FilterTriplesBySubjectBase keeps triples whose subject IRI starts with iriBase
// (longest-match among allBases when provided).
func FilterTriplesBySubjectBase(triples []Triple, iriBase string, allBases []string) []Triple {
	out := make([]Triple, 0)
	for _, tr := range triples {
		if tr.Subject.Kind != TermIRI {
			continue
		}
		match := iriBase
		if len(allBases) > 0 {
			match = LongestMatchingBase(tr.Subject.Value, allBases)
		}
		if match == iriBase && strings.HasPrefix(tr.Subject.Value, iriBase) {
			out = append(out, tr)
		}
	}
	return out
}

// SerializeNTriples writes triples as N-Triples text.
func SerializeNTriples(triples []Triple) string {
	var b strings.Builder
	for _, tr := range triples {
		fmt.Fprintf(&b, "%s %s %s .\n", formatTerm(tr.Subject), formatTerm(tr.Predicate), formatTerm(tr.Object))
	}
	return b.String()
}

func formatTerm(t Term) string {
	switch t.Kind {
	case TermIRI:
		return "<" + t.Value + ">"
	case TermBlank:
		return "_:" + t.Value
	case TermLiteral:
		esc := strings.Builder{}
		esc.WriteByte('"')
		for _, r := range t.Value {
			switch r {
			case '\\':
				esc.WriteString(`\\`)
			case '"':
				esc.WriteString(`\"`)
			case '\n':
				esc.WriteString(`\n`)
			case '\r':
				esc.WriteString(`\r`)
			case '\t':
				esc.WriteString(`\t`)
			default:
				esc.WriteRune(r)
			}
		}
		esc.WriteByte('"')
		s := esc.String()
		if t.Lang != "" {
			return s + "@" + t.Lang
		}
		if t.Datatype != "" {
			return s + "^^<" + t.Datatype + ">"
		}
		return s
	default:
		return `""`
	}
}
