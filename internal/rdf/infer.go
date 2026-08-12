package rdf

import (
	"strconv"
	"strings"

	"github.com/l-ra/knowledge-core/internal/datatype"
)

const (
	IRIType      = "http://www.w3.org/1999/02/22-rdf-syntax-ns#type"
	IRIProperty  = "http://www.w3.org/1999/02/22-rdf-syntax-ns#Property"
	IRILabel     = "http://www.w3.org/2000/01/rdf-schema#label"
	IRIClass     = "http://www.w3.org/2000/01/rdf-schema#Class"
	IRISubClass  = "http://www.w3.org/2000/01/rdf-schema#subClassOf"
	IRIDomain    = "http://www.w3.org/2000/01/rdf-schema#domain"
	IRIRange     = "http://www.w3.org/2000/01/rdf-schema#range"
	IRISameAs    = "http://www.w3.org/2002/07/owl#sameAs"
	IRIOwlClass  = "http://www.w3.org/2002/07/owl#Class"
	IRILiteral   = "http://www.w3.org/2000/01/rdf-schema#Literal"
	IRIResource  = "http://www.w3.org/2000/01/rdf-schema#Resource"
	IRIThing     = "http://www.w3.org/2002/07/owl#Thing"
	IRIXSDString = "http://www.w3.org/2001/XMLSchema#string"
	IRIXSDBool   = "http://www.w3.org/2001/XMLSchema#boolean"
	IRIXSDInt    = "http://www.w3.org/2001/XMLSchema#integer"
	IRIXSDLong   = "http://www.w3.org/2001/XMLSchema#long"
	IRIXSDDec    = "http://www.w3.org/2001/XMLSchema#decimal"
	IRIXSDDate   = "http://www.w3.org/2001/XMLSchema#date"
	IRIXSDDT     = "http://www.w3.org/2001/XMLSchema#dateTime"
	IRIXSDAnyURI = "http://www.w3.org/2001/XMLSchema#anyURI"
	IRILangStr   = "http://www.w3.org/1999/02/22-rdf-syntax-ns#langString"
)

// Kind of a graph node for import planning.
type NodeKind string

const (
	KindEntity   NodeKind = "entity"
	KindProperty NodeKind = "property"
	KindClass    NodeKind = "class"
)

// InferPropertyDatatype chooses a platform datatype from rdfs:range IRIs and observed value kinds.
// Conflicting evidence or rdfs:Literal → Any.
func InferPropertyDatatype(ranges []string, observed []datatype.Type) datatype.Type {
	set := map[datatype.Type]struct{}{}
	for _, r := range ranges {
		if t, ok := rangeToDatatype(r); ok {
			if t == datatype.Any {
				return datatype.Any
			}
			set[t] = struct{}{}
		}
	}
	for _, o := range observed {
		if o == "" {
			continue
		}
		set[o] = struct{}{}
	}
	if len(set) == 0 {
		return datatype.String
	}
	if len(set) > 1 {
		return datatype.Any
	}
	for t := range set {
		return t
	}
	return datatype.String
}

func rangeToDatatype(iri string) (datatype.Type, bool) {
	switch iri {
	case IRIXSDString:
		return datatype.String, true
	case IRILangStr:
		return datatype.LocalizedString, true
	case IRIXSDBool:
		return datatype.Boolean, true
	case IRIXSDInt, IRIXSDLong:
		return datatype.Integer, true
	case IRIXSDDec:
		return datatype.Decimal, true
	case IRIXSDDate:
		return datatype.Date, true
	case IRIXSDDT:
		return datatype.DateTime, true
	case IRIXSDAnyURI:
		return datatype.URI, true
	case IRILiteral:
		return datatype.Any, true
	case IRIResource, IRIThing, IRIClass, IRIOwlClass:
		return datatype.EntityReference, true
	default:
		// custom class / resource range → EntityReference
		if strings.HasPrefix(iri, "http://") || strings.HasPrefix(iri, "https://") {
			return datatype.EntityReference, true
		}
		return "", false
	}
}

// LiteralToValue maps an RDF literal to a platform value (+ observed datatype).
func LiteralToValue(obj Term) (datatype.Value, datatype.Type, error) {
	if obj.Kind != TermLiteral {
		return datatype.Value{}, "", nil
	}
	if obj.Lang != "" {
		return datatype.Value{
			Type:    datatype.LocalizedString,
			LangMap: map[string]string{obj.Lang: obj.Value},
		}, datatype.LocalizedString, nil
	}
	switch obj.Datatype {
	case "", IRIXSDString:
		s := obj.Value
		return datatype.Value{Type: datatype.String, String: &s}, datatype.String, nil
	case IRIXSDBool:
		b := obj.Value == "true" || obj.Value == "1"
		return datatype.Value{Type: datatype.Boolean, Bool: &b}, datatype.Boolean, nil
	case IRIXSDInt, IRIXSDLong:
		parsed, err := strconv.ParseInt(obj.Value, 10, 64)
		if err != nil {
			s := obj.Value
			return datatype.Value{Type: datatype.String, String: &s}, datatype.String, nil
		}
		return datatype.Value{Type: datatype.Integer, Int64: &parsed}, datatype.Integer, nil
	case IRIXSDDec:
		s := obj.Value
		return datatype.Value{Type: datatype.Decimal, Decimal: &s}, datatype.Decimal, nil
	case IRIXSDDate:
		s := obj.Value
		return datatype.Value{Type: datatype.Date, Date: &s}, datatype.Date, nil
	case IRIXSDDT:
		s := obj.Value
		return datatype.Value{Type: datatype.DateTime, DateTime: &s}, datatype.DateTime, nil
	case IRIXSDAnyURI:
		s := obj.Value
		return datatype.Value{Type: datatype.URI, URI: &s}, datatype.URI, nil
	default:
		s := obj.Value
		return datatype.Value{Type: datatype.String, String: &s}, datatype.String, nil
	}
}

// IsStructuralPredicate returns true for triples handled specially (not as statements).
func IsStructuralPredicate(pred string) bool {
	switch pred {
	case IRIType, IRILabel, IRISameAs, IRISubClass, IRIDomain, IRIRange:
		return true
	default:
		return false
	}
}

// ClassifyNode picks entity/property/class from rdf:type objects.
func ClassifyNode(typeIRIs []string) NodeKind {
	isProp, isClass := false, false
	for _, t := range typeIRIs {
		switch t {
		case IRIProperty:
			isProp = true
		case IRIClass, IRIOwlClass:
			isClass = true
		}
	}
	if isProp && !isClass {
		return KindProperty
	}
	if isClass && !isProp {
		return KindClass
	}
	if isProp && isClass {
		// Prefer property when both asserted (unusual).
		return KindProperty
	}
	return KindEntity
}

// LocalNameFromIRI extracts a short label candidate from an IRI.
func LocalNameFromIRI(iri string) string {
	if i := strings.LastIndexAny(iri, "/#"); i >= 0 && i+1 < len(iri) {
		return iri[i+1:]
	}
	return iri
}
