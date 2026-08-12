package rdf

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Term kinds in a parsed triple.
const (
	TermIRI     = "iri"
	TermLiteral = "literal"
	TermBlank   = "blank"
)

// Term is an RDF term (IRI, literal, or blank).
type Term struct {
	Kind     string
	Value    string // IRI, literal lexical form, or blank id
	Lang     string
	Datatype string // absolute datatype IRI for typed literals
}

// Triple is one N-Triples statement.
type Triple struct {
	Subject   Term
	Predicate Term
	Object    Term
	Line      int
}

// ParseNTriples parses N-Triples text. Blank nodes are allowed in the AST
// but import rejects them.
func ParseNTriples(input string) ([]Triple, error) {
	lines := strings.Split(input, "\n")
	out := make([]Triple, 0)
	for i, raw := range lines {
		lineNo := i + 1
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		tr, err := parseTripleLine(line, lineNo)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNo, err)
		}
		out = append(out, tr)
	}
	return out, nil
}

func parseTripleLine(line string, lineNo int) (Triple, error) {
	rest := line
	subj, rest, err := parseSubject(rest)
	if err != nil {
		return Triple{}, err
	}
	rest = strings.TrimLeftFunc(rest, unicode.IsSpace)
	pred, rest, err := parseIRITerm(rest)
	if err != nil {
		return Triple{}, fmt.Errorf("predicate: %w", err)
	}
	rest = strings.TrimLeftFunc(rest, unicode.IsSpace)
	obj, rest, err := parseObject(rest)
	if err != nil {
		return Triple{}, fmt.Errorf("object: %w", err)
	}
	rest = strings.TrimLeftFunc(rest, unicode.IsSpace)
	if !strings.HasPrefix(rest, ".") {
		return Triple{}, fmt.Errorf("expected '.' terminator")
	}
	rest = strings.TrimSpace(rest[1:])
	if rest != "" && !strings.HasPrefix(rest, "#") {
		return Triple{}, fmt.Errorf("trailing junk after triple")
	}
	return Triple{Subject: subj, Predicate: pred, Object: obj, Line: lineNo}, nil
}

func parseSubject(s string) (Term, string, error) {
	if strings.HasPrefix(s, "_:") {
		id, rest := readBlank(s)
		return Term{Kind: TermBlank, Value: id}, rest, nil
	}
	return parseIRITerm(s)
}

func parseObject(s string) (Term, string, error) {
	if strings.HasPrefix(s, "_:") {
		id, rest := readBlank(s)
		return Term{Kind: TermBlank, Value: id}, rest, nil
	}
	if strings.HasPrefix(s, "<") {
		return parseIRITerm(s)
	}
	if strings.HasPrefix(s, "\"") {
		return parseLiteral(s)
	}
	return Term{}, s, fmt.Errorf("unsupported object term")
}

func parseIRITerm(s string) (Term, string, error) {
	if !strings.HasPrefix(s, "<") {
		return Term{}, s, fmt.Errorf("expected IRI in <>")
	}
	end := strings.IndexByte(s, '>')
	if end < 0 {
		return Term{}, s, fmt.Errorf("unclosed IRI")
	}
	iri := s[1:end]
	if iri == "" || strings.ContainsAny(iri, " \t\n\r<>\"{}|^`\\") {
		return Term{}, s, fmt.Errorf("invalid IRI")
	}
	return Term{Kind: TermIRI, Value: unescapeIRI(iri)}, s[end+1:], nil
}

func readBlank(s string) (string, string) {
	i := 2
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		if unicode.IsSpace(r) || r == '.' {
			break
		}
		i += size
	}
	return s[2:i], s[i:]
}

func parseLiteral(s string) (Term, string, error) {
	if !strings.HasPrefix(s, "\"") {
		return Term{}, s, fmt.Errorf("expected quoted literal")
	}
	lex, rest, err := readQuoted(s)
	if err != nil {
		return Term{}, s, err
	}
	t := Term{Kind: TermLiteral, Value: lex}
	rest = strings.TrimLeftFunc(rest, unicode.IsSpace)
	if strings.HasPrefix(rest, "@") {
		lang, rem := readLang(rest[1:])
		if lang == "" {
			return Term{}, s, fmt.Errorf("empty language tag")
		}
		t.Lang = strings.ToLower(lang)
		return t, rem, nil
	}
	if strings.HasPrefix(rest, "^^") {
		rest = rest[2:]
		rest = strings.TrimLeftFunc(rest, unicode.IsSpace)
		dt, rem, err := parseIRITerm(rest)
		if err != nil {
			return Term{}, s, fmt.Errorf("datatype: %w", err)
		}
		t.Datatype = dt.Value
		return t, rem, nil
	}
	return t, rest, nil
}

func readQuoted(s string) (string, string, error) {
	var b strings.Builder
	i := 1
	for i < len(s) {
		ch := s[i]
		if ch == '"' {
			return b.String(), s[i+1:], nil
		}
		if ch == '\\' {
			if i+1 >= len(s) {
				return "", s, fmt.Errorf("truncated escape")
			}
			n := s[i+1]
			switch n {
			case 't':
				b.WriteByte('\t')
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case '"', '\\':
				b.WriteByte(n)
			case 'u':
				if i+6 >= len(s) {
					return "", s, fmt.Errorf("bad \\u escape")
				}
				r, err := strconv.ParseInt(s[i+2:i+6], 16, 32)
				if err != nil {
					return "", s, fmt.Errorf("bad \\u escape")
				}
				b.WriteRune(rune(r))
				i += 6
				continue
			case 'U':
				if i+10 >= len(s) {
					return "", s, fmt.Errorf("bad \\U escape")
				}
				r, err := strconv.ParseInt(s[i+2:i+10], 16, 32)
				if err != nil {
					return "", s, fmt.Errorf("bad \\U escape")
				}
				b.WriteRune(rune(r))
				i += 10
				continue
			default:
				return "", s, fmt.Errorf("unknown escape \\%c", n)
			}
			i += 2
			continue
		}
		b.WriteByte(ch)
		i++
	}
	return "", s, fmt.Errorf("unclosed literal")
}

func readLang(s string) (string, string) {
	i := 0
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' {
			i += size
			continue
		}
		break
	}
	return s[:i], s[i:]
}

func unescapeIRI(s string) string {
	// N-Triples IRIs may contain \u / \U escapes; keep simple path for v1.
	return s
}
