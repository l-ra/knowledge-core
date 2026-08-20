// Package pkgcompat compares two package snapshots for backward-compatible upgrades.
package pkgcompat

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/l-ra/knowledge-core/internal/datatype"
	"github.com/l-ra/knowledge-core/internal/domain"
)

type Kind string

const (
	KindAdditive  Kind = "additive"
	KindMetadata  Kind = "metadata"
	KindBreaking  Kind = "breaking"
)

// Reason codes for structured API findings.
const (
	ReasonObjectAdded       = "object_added"
	ReasonObjectRemoved     = "object_removed"
	ReasonDatatypeChanged   = "datatype_changed"
	ReasonConstraintTighten = "constraint_tightened"
	ReasonConstraintLoosen  = "constraint_loosened"
	ReasonSubClassChanged   = "subclass_changed"
	ReasonShapeTightened    = "shape_tightened"
	ReasonShapeLoosened     = "shape_loosened"
	ReasonLabelsChanged     = "labels_changed"
	ReasonStatusDeprecated  = "status_deprecated"
	ReasonStatusBreaking    = "status_breaking"
	ReasonStatementChanged  = "statement_changed"
	ReasonEntityChanged     = "entity_changed"
	ReasonIRILocalChanged   = "iri_local_changed"
)

type Finding struct {
	Kind            Kind   `json:"kind"`
	ReasonCode      string `json:"reasonCode"`
	ObjectType      string `json:"objectType"`
	ObjectPublicID  string `json:"objectPublicId"`
	Detail          string `json:"detail,omitempty"`
}

func (f Finding) String() string {
	return fmt.Sprintf("%s %s %s %s: %s", f.Kind, f.ObjectType, f.ObjectPublicID, f.ReasonCode, f.Detail)
}

// Snapshot is a comparable view of one package version (bundle or live export).
type Snapshot struct {
	Package    string
	Version    string
	Entities   map[string]domain.BundleEntity
	Properties map[string]domain.BundleProperty
	Classes    map[string]domain.BundleClass
	Shapes     map[string]domain.BundleShape
	Statements map[string]domain.BundleStatement
}

func SnapshotFromBundle(b domain.Bundle) Snapshot {
	s := Snapshot{
		Package:    b.Manifest.Package,
		Version:    b.Manifest.Version,
		Entities:   map[string]domain.BundleEntity{},
		Properties: map[string]domain.BundleProperty{},
		Classes:    map[string]domain.BundleClass{},
		Shapes:     map[string]domain.BundleShape{},
		Statements: map[string]domain.BundleStatement{},
	}
	for _, e := range b.Entities {
		s.Entities[e.PublicID] = e
	}
	for _, p := range b.Properties {
		s.Properties[p.PublicID] = p
	}
	for _, c := range b.Classes {
		s.Classes[c.PublicID] = c
	}
	for _, sh := range b.Shapes {
		s.Shapes[sh.Code] = sh
	}
	for _, st := range b.Statements {
		s.Statements[st.PublicID] = st
	}
	return s
}

func HasBreaking(findings []Finding) bool {
	for _, f := range findings {
		if f.Kind == KindBreaking {
			return true
		}
	}
	return false
}

func BreakingFindings(findings []Finding) []Finding {
	var out []Finding
	for _, f := range findings {
		if f.Kind == KindBreaking {
			out = append(out, f)
		}
	}
	return out
}

// Diff reports changes from old → new. Missing objects in new that existed in old are breaking.
func Diff(old, new Snapshot) []Finding {
	var out []Finding

	diffMaps(
		keys(old.Classes), keys(new.Classes), "class",
		func(id string) { out = append(out, Finding{Kind: KindAdditive, ReasonCode: ReasonObjectAdded, ObjectType: "class", ObjectPublicID: id}) },
		func(id string) {
			out = append(out, Finding{Kind: KindBreaking, ReasonCode: ReasonObjectRemoved, ObjectType: "class", ObjectPublicID: id, Detail: "class absent in new snapshot"})
		},
		func(id string) { out = append(out, diffClass(old.Classes[id], new.Classes[id])...) },
	)
	diffMaps(
		keys(old.Properties), keys(new.Properties), "property",
		func(id string) { out = append(out, Finding{Kind: KindAdditive, ReasonCode: ReasonObjectAdded, ObjectType: "property", ObjectPublicID: id}) },
		func(id string) {
			out = append(out, Finding{Kind: KindBreaking, ReasonCode: ReasonObjectRemoved, ObjectType: "property", ObjectPublicID: id, Detail: "property absent in new snapshot"})
		},
		func(id string) { out = append(out, diffProperty(old.Properties[id], new.Properties[id])...) },
	)
	diffMaps(
		keys(old.Entities), keys(new.Entities), "entity",
		func(id string) { out = append(out, Finding{Kind: KindAdditive, ReasonCode: ReasonObjectAdded, ObjectType: "entity", ObjectPublicID: id}) },
		func(id string) {
			out = append(out, Finding{Kind: KindBreaking, ReasonCode: ReasonObjectRemoved, ObjectType: "entity", ObjectPublicID: id, Detail: "entity absent in new snapshot"})
		},
		func(id string) { out = append(out, diffEntity(old.Entities[id], new.Entities[id])...) },
	)
	diffMaps(
		keys(old.Shapes), keys(new.Shapes), "shape",
		func(id string) { out = append(out, Finding{Kind: KindAdditive, ReasonCode: ReasonObjectAdded, ObjectType: "shape", ObjectPublicID: id}) },
		func(id string) {
			out = append(out, Finding{Kind: KindBreaking, ReasonCode: ReasonObjectRemoved, ObjectType: "shape", ObjectPublicID: id, Detail: "shape absent in new snapshot"})
		},
		func(id string) { out = append(out, diffShape(old.Shapes[id], new.Shapes[id])...) },
	)
	diffMaps(
		keys(old.Statements), keys(new.Statements), "statement",
		func(id string) { out = append(out, Finding{Kind: KindAdditive, ReasonCode: ReasonObjectAdded, ObjectType: "statement", ObjectPublicID: id}) },
		func(id string) {
			out = append(out, Finding{Kind: KindBreaking, ReasonCode: ReasonObjectRemoved, ObjectType: "statement", ObjectPublicID: id, Detail: "statement absent in new snapshot"})
		},
		func(id string) { out = append(out, diffStatement(old.Statements[id], new.Statements[id], new.Properties)...) },
	)

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		if out[i].ObjectType != out[j].ObjectType {
			return out[i].ObjectType < out[j].ObjectType
		}
		return out[i].ObjectPublicID < out[j].ObjectPublicID
	})
	return out
}

func diffMaps(oldKeys, newKeys []string, _ string, onAdd, onRemove, onBoth func(id string)) {
	oldSet := toSet(oldKeys)
	newSet := toSet(newKeys)
	for _, id := range newKeys {
		if _, ok := oldSet[id]; !ok {
			onAdd(id)
		}
	}
	for _, id := range oldKeys {
		if _, ok := newSet[id]; !ok {
			onRemove(id)
		} else {
			onBoth(id)
		}
	}
}

func diffClass(o, n domain.BundleClass) []Finding {
	var out []Finding
	id := n.PublicID
	if o.IRILocal != n.IRILocal {
		out = append(out, Finding{Kind: KindBreaking, ReasonCode: ReasonIRILocalChanged, ObjectType: "class", ObjectPublicID: id,
			Detail: fmt.Sprintf("iriLocal %q → %q", o.IRILocal, n.IRILocal)})
	}
	if o.SubClassOf != n.SubClassOf {
		if o.SubClassOf == "" && n.SubClassOf != "" {
			out = append(out, Finding{Kind: KindAdditive, ReasonCode: ReasonSubClassChanged, ObjectType: "class", ObjectPublicID: id,
				Detail: fmt.Sprintf("subClassOf set to %q", n.SubClassOf)})
		} else {
			out = append(out, Finding{Kind: KindBreaking, ReasonCode: ReasonSubClassChanged, ObjectType: "class", ObjectPublicID: id,
				Detail: fmt.Sprintf("subClassOf %q → %q", o.SubClassOf, n.SubClassOf)})
		}
	}
	out = append(out, statusFindings("class", id, string(o.Status), string(n.Status))...)
	if !mapsEqual(o.Labels, n.Labels) || !mapsEqual(o.Descriptions, n.Descriptions) {
		out = append(out, Finding{Kind: KindMetadata, ReasonCode: ReasonLabelsChanged, ObjectType: "class", ObjectPublicID: id})
	}
	return out
}

func diffProperty(o, n domain.BundleProperty) []Finding {
	var out []Finding
	id := n.PublicID
	if o.IRILocal != n.IRILocal {
		out = append(out, Finding{Kind: KindBreaking, ReasonCode: ReasonIRILocalChanged, ObjectType: "property", ObjectPublicID: id,
			Detail: fmt.Sprintf("iriLocal %q → %q", o.IRILocal, n.IRILocal)})
	}
	if o.Datatype != n.Datatype {
		out = append(out, Finding{Kind: KindBreaking, ReasonCode: ReasonDatatypeChanged, ObjectType: "property", ObjectPublicID: id,
			Detail: fmt.Sprintf("%s → %s", o.Datatype, n.Datatype)})
	}
	out = append(out, diffConstraints(id, o.Constraints, n.Constraints)...)
	out = append(out, statusFindings("property", id, string(o.Status), string(n.Status))...)
	if !mapsEqual(o.Labels, n.Labels) || !mapsEqual(o.Descriptions, n.Descriptions) {
		out = append(out, Finding{Kind: KindMetadata, ReasonCode: ReasonLabelsChanged, ObjectType: "property", ObjectPublicID: id})
	}
	return out
}

func diffConstraints(propID string, o, n domain.PropertyConstraints) []Finding {
	var out []Finding
	add := func(kind Kind, code, detail string) {
		out = append(out, Finding{Kind: kind, ReasonCode: code, ObjectType: "property", ObjectPublicID: propID, Detail: detail})
	}
	if intPtrGreater(n.MinCount, o.MinCount) {
		add(KindBreaking, ReasonConstraintTighten, fmt.Sprintf("minCount %v → %v", fmtInt(o.MinCount), fmtInt(n.MinCount)))
	} else if intPtrGreater(o.MinCount, n.MinCount) {
		add(KindAdditive, ReasonConstraintLoosen, fmt.Sprintf("minCount %v → %v", fmtInt(o.MinCount), fmtInt(n.MinCount)))
	}
	// maxCount: nil = unbounded; lower bound is stricter
	if maxTightened(o.MaxCount, n.MaxCount) {
		add(KindBreaking, ReasonConstraintTighten, fmt.Sprintf("maxCount %v → %v", fmtInt(o.MaxCount), fmtInt(n.MaxCount)))
	} else if maxLoosened(o.MaxCount, n.MaxCount) {
		add(KindAdditive, ReasonConstraintLoosen, fmt.Sprintf("maxCount %v → %v", fmtInt(o.MaxCount), fmtInt(n.MaxCount)))
	}
	if domainRangeTightened(o.DomainClasses, n.DomainClasses) {
		add(KindBreaking, ReasonConstraintTighten, "domainClasses narrowed")
	} else if domainRangeLoosened(o.DomainClasses, n.DomainClasses) {
		add(KindAdditive, ReasonConstraintLoosen, "domainClasses expanded")
	}
	if domainRangeTightened(o.RangeClasses, n.RangeClasses) {
		add(KindBreaking, ReasonConstraintTighten, "rangeClasses narrowed")
	} else if domainRangeLoosened(o.RangeClasses, n.RangeClasses) {
		add(KindAdditive, ReasonConstraintLoosen, "rangeClasses expanded")
	}
	if sevRank(n.Severity) > sevRank(o.Severity) {
		add(KindBreaking, ReasonConstraintTighten, fmt.Sprintf("severity %q → %q", o.Severity, n.Severity))
	} else if sevRank(n.Severity) < sevRank(o.Severity) {
		add(KindAdditive, ReasonConstraintLoosen, fmt.Sprintf("severity %q → %q", o.Severity, n.Severity))
	}
	return out
}

func diffShape(o, n domain.BundleShape) []Finding {
	var out []Finding
	id := n.Code
	if o.ClassID != n.ClassID {
		out = append(out, Finding{Kind: KindBreaking, ReasonCode: ReasonShapeTightened, ObjectType: "shape", ObjectPublicID: id,
			Detail: fmt.Sprintf("classId %q → %q", o.ClassID, n.ClassID)})
	}
	od, nd := o.Document, n.Document
	if !stringSetSubset(nd.RequiredProperties, od.RequiredProperties) {
		// new required is not subset of old required → added requirements
		out = append(out, Finding{Kind: KindBreaking, ReasonCode: ReasonShapeTightened, ObjectType: "shape", ObjectPublicID: id,
			Detail: "requiredProperties added"})
	} else if !stringSetEqual(od.RequiredProperties, nd.RequiredProperties) {
		out = append(out, Finding{Kind: KindAdditive, ReasonCode: ReasonShapeLoosened, ObjectType: "shape", ObjectPublicID: id,
			Detail: "requiredProperties removed"})
	}
	if !od.Closed && nd.Closed {
		out = append(out, Finding{Kind: KindBreaking, ReasonCode: ReasonShapeTightened, ObjectType: "shape", ObjectPublicID: id, Detail: "closed true"})
	} else if od.Closed && !nd.Closed {
		out = append(out, Finding{Kind: KindAdditive, ReasonCode: ReasonShapeLoosened, ObjectType: "shape", ObjectPublicID: id, Detail: "closed false"})
	}
	if sevRank(nd.Severity) > sevRank(od.Severity) {
		out = append(out, Finding{Kind: KindBreaking, ReasonCode: ReasonShapeTightened, ObjectType: "shape", ObjectPublicID: id,
			Detail: fmt.Sprintf("severity %q → %q", od.Severity, nd.Severity)})
	} else if sevRank(nd.Severity) < sevRank(od.Severity) {
		out = append(out, Finding{Kind: KindAdditive, ReasonCode: ReasonShapeLoosened, ObjectType: "shape", ObjectPublicID: id,
			Detail: fmt.Sprintf("severity %q → %q", od.Severity, nd.Severity)})
	}
	// AllowedProperties: narrowing (new not superset of old when both non-empty) is breaking
	if len(od.AllowedProperties) > 0 || len(nd.AllowedProperties) > 0 {
		if len(nd.AllowedProperties) > 0 && !stringSetSubset(od.AllowedProperties, nd.AllowedProperties) && len(od.AllowedProperties) > 0 {
			out = append(out, Finding{Kind: KindBreaking, ReasonCode: ReasonShapeTightened, ObjectType: "shape", ObjectPublicID: id,
				Detail: "allowedProperties narrowed"})
		} else if len(od.AllowedProperties) > 0 && len(nd.AllowedProperties) == 0 {
			out = append(out, Finding{Kind: KindAdditive, ReasonCode: ReasonShapeLoosened, ObjectType: "shape", ObjectPublicID: id,
				Detail: "allowedProperties cleared"})
		} else if !stringSetEqual(od.AllowedProperties, nd.AllowedProperties) {
			out = append(out, Finding{Kind: KindAdditive, ReasonCode: ReasonShapeLoosened, ObjectType: "shape", ObjectPublicID: id,
				Detail: "allowedProperties expanded"})
		}
	}
	return out
}

func diffEntity(o, n domain.BundleEntity) []Finding {
	var out []Finding
	id := n.PublicID
	if o.IRILocal != n.IRILocal {
		out = append(out, Finding{Kind: KindBreaking, ReasonCode: ReasonIRILocalChanged, ObjectType: "entity", ObjectPublicID: id,
			Detail: fmt.Sprintf("iriLocal %q → %q", o.IRILocal, n.IRILocal)})
	}
	out = append(out, statusFindings("entity", id, string(o.Status), string(n.Status))...)
	if !mapsEqual(o.Labels, n.Labels) || !mapsEqual(o.Descriptions, n.Descriptions) {
		out = append(out, Finding{Kind: KindMetadata, ReasonCode: ReasonLabelsChanged, ObjectType: "entity", ObjectPublicID: id})
	}
	return out
}

func diffStatement(o, n domain.BundleStatement, props map[string]domain.BundleProperty) []Finding {
	if statementEqual(o, n) {
		return nil
	}
	kind := KindMetadata
	reason := ReasonStatementChanged
	detail := "statement content changed"
	// Structural policy predicates: changing them is breaking.
	propLocal := propertyLocal(n.Property, props)
	if propLocal == "" {
		propLocal = propertyLocal(o.Property, props)
	}
	if isStructuralProperty(propLocal) || o.Subject != n.Subject || o.Property != n.Property {
		kind = KindBreaking
	}
	if string(o.Status) == "active" && string(n.Status) == "deleted" {
		kind = KindBreaking
		reason = ReasonStatusBreaking
		detail = "statement deleted"
	}
	return []Finding{{Kind: kind, ReasonCode: reason, ObjectType: "statement", ObjectPublicID: n.PublicID, Detail: detail}}
}

func isStructuralProperty(iriLocal string) bool {
	switch iriLocal {
	case "allowedRelType", "allowedSourceClass", "allowedTargetClass",
		"enumeratesProperty", "allowedValue",
		"relSource", "relTarget",
		"instanceOf":
		return true
	default:
		return false
	}
}

func propertyLocal(pid string, props map[string]domain.BundleProperty) string {
	if p, ok := props[pid]; ok {
		return p.IRILocal
	}
	// Fall back: last path segment of IRI
	for i := len(pid) - 1; i >= 0; i-- {
		if pid[i] == '/' {
			return pid[i+1:]
		}
	}
	return pid
}

func statementEqual(o, n domain.BundleStatement) bool {
	if o.Subject != n.Subject || o.Property != n.Property || o.Status != n.Status {
		return false
	}
	if !valuesEqual(o.Value, n.Value) {
		return false
	}
	if len(o.ReferenceIDs) != len(n.ReferenceIDs) {
		return false
	}
	for i := range o.ReferenceIDs {
		if o.ReferenceIDs[i] != n.ReferenceIDs[i] {
			return false
		}
	}
	if len(o.Qualifiers) != len(n.Qualifiers) {
		return false
	}
	for i := range o.Qualifiers {
		if o.Qualifiers[i].Property != n.Qualifiers[i].Property || !valuesEqual(o.Qualifiers[i].Value, n.Qualifiers[i].Value) {
			return false
		}
	}
	return true
}

func valuesEqual(a, b datatype.Value) bool {
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	return string(aj) == string(bj)
}

func statusFindings(objectType, id, oldS, newS string) []Finding {
	if oldS == newS {
		return nil
	}
	if newS == "deprecated" && oldS == "active" {
		return []Finding{{Kind: KindMetadata, ReasonCode: ReasonStatusDeprecated, ObjectType: objectType, ObjectPublicID: id,
			Detail: fmt.Sprintf("%s → %s", oldS, newS)}}
	}
	if newS == "deleted" || oldS == "deleted" {
		return []Finding{{Kind: KindBreaking, ReasonCode: ReasonStatusBreaking, ObjectType: objectType, ObjectPublicID: id,
			Detail: fmt.Sprintf("%s → %s", oldS, newS)}}
	}
	return []Finding{{Kind: KindMetadata, ReasonCode: ReasonEntityChanged, ObjectType: objectType, ObjectPublicID: id,
		Detail: fmt.Sprintf("status %s → %s", oldS, newS)}}
}

func keys[T any](m map[string]T) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func toSet(ks []string) map[string]struct{} {
	m := make(map[string]struct{}, len(ks))
	for _, k := range ks {
		m[k] = struct{}{}
	}
	return m
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func stringSetEqual(a, b []string) bool {
	return stringSetSubset(a, b) && stringSetSubset(b, a)
}

// Empty domain/range means unrestricted. Restricting or removing allowed classes is tightening.
func domainRangeTightened(old, new []string) bool {
	if stringSetEqual(old, new) {
		return false
	}
	if len(old) == 0 && len(new) > 0 {
		return true
	}
	if len(new) == 0 {
		return false
	}
	// old must be subset of new for BC; otherwise narrowed or replaced
	return !stringSetSubset(old, new)
}

func domainRangeLoosened(old, new []string) bool {
	if stringSetEqual(old, new) {
		return false
	}
	if len(old) > 0 && len(new) == 0 {
		return true
	}
	if len(old) == 0 {
		return false
	}
	return stringSetSubset(old, new)
}

// stringSetSubset reports whether every element of a is in b.
func stringSetSubset(a, b []string) bool {
	if len(a) == 0 {
		return true
	}
	bs := toSet(b)
	for _, x := range a {
		if _, ok := bs[x]; !ok {
			return false
		}
	}
	return true
}

func intPtrGreater(a, b *int) bool {
	// a > b treating nil as 0 for minCount
	av, bv := 0, 0
	if a != nil {
		av = *a
	}
	if b != nil {
		bv = *b
	}
	return av > bv
}

func maxTightened(old, new *int) bool {
	// unbounded (nil) → finite = tighten; finite → smaller finite = tighten
	if old == nil && new != nil {
		return true
	}
	if old != nil && new != nil && *new < *old {
		return true
	}
	return false
}

func maxLoosened(old, new *int) bool {
	if old != nil && new == nil {
		return true
	}
	if old != nil && new != nil && *new > *old {
		return true
	}
	return false
}

func fmtInt(p *int) string {
	if p == nil {
		return "nil"
	}
	return fmt.Sprintf("%d", *p)
}

func sevRank(s domain.ValidationSeverity) int {
	switch s {
	case domain.SeverityError:
		return 3
	case domain.SeverityWarning:
		return 2
	case domain.SeverityInfo:
		return 1
	default:
		return 0
	}
}
