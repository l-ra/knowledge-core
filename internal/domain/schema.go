package domain

import "strings"

type ValidationSeverity string

const (
	SeverityError   ValidationSeverity = "error"
	SeverityWarning ValidationSeverity = "warning"
	SeverityInfo    ValidationSeverity = "info"
)

type ValidationMode string

const (
	ValidationOff     ValidationMode = "off"
	ValidationRelaxed ValidationMode = "relaxed"
	ValidationStrict  ValidationMode = "strict"
)

func ParseValidationMode(s string) ValidationMode {
	switch ValidationMode(s) {
	case ValidationOff, ValidationStrict:
		return ValidationMode(s)
	default:
		return ValidationRelaxed
	}
}

type PropertyConstraints struct {
	DomainClasses []string           `json:"domainClasses,omitempty"`
	RangeClasses  []string           `json:"rangeClasses,omitempty"`
	MinCount      *int               `json:"minCount,omitempty"`
	MaxCount      *int               `json:"maxCount,omitempty"`
	Severity      ValidationSeverity `json:"severity,omitempty"`
}

type ClassDocument struct {
	SubClassOf string `json:"subClassOf,omitempty"`
}

type ClassDefinition struct {
	ID                 string
	PublicID           string
	Status             PropertyStatus
	PackageCode        string
	IRILocal           string
	IRI                string
	CanonicalEntityID  string
	CanonicalEntityQID string
	Document           ClassDocument
	Labels             map[string]string
	Descriptions       map[string]string
	EffectiveClasses   []string
	CreatedAt          string
	UpdatedAt          string
}

type CreateClassInput struct {
	PackageCode       string
	Labels            map[string]string
	Descriptions      map[string]string
	SubClassOf        string
	CanonicalEntityID string
	IRILocal          string
}

type ShapeDocument struct {
	RequiredProperties []string           `json:"requiredProperties,omitempty"`
	AllowedProperties  []string           `json:"allowedProperties,omitempty"`
	Closed             bool               `json:"closed,omitempty"`
	Severity           ValidationSeverity `json:"severity,omitempty"`
}

type ShapeProfile struct {
	ID          string
	Code        string
	ClassID     string
	ClassPID    string
	PackageCode string
	Document    ShapeDocument
	CreatedAt   string
	UpdatedAt   string
}

type CreateShapeInput struct {
	Code        string
	ClassID     string
	PackageCode string
	Document    ShapeDocument
}

// WellKnownInstanceOfPropertyIRI is the canonical kc-base typing property.
// When this property is ingested and schema-config.instanceOfProperty is empty,
// the store sets it automatically.
const WellKnownInstanceOfPropertyIRI = "https://knowledge-core.local/kc-base/instanceOf"

type ModelSchemaConfig struct {
	InstanceOfProperty string   `json:"instanceOfProperty"`
	ModelProperties    []string `json:"modelProperties"`
	UpdatedAt          string   `json:"updatedAt,omitempty"`
}

// EffectiveModelProperties returns configured model PIDs plus instanceOf when set.
func (c ModelSchemaConfig) EffectiveModelProperties() []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(pid string) {
		pid = strings.TrimSpace(pid)
		if pid == "" {
			return
		}
		if _, ok := seen[pid]; ok {
			return
		}
		seen[pid] = struct{}{}
		out = append(out, pid)
	}
	for _, pid := range c.ModelProperties {
		add(pid)
	}
	add(c.InstanceOfProperty)
	return out
}

type ValidationFinding struct {
	Code        string             `json:"code"`
	Severity    ValidationSeverity `json:"severity"`
	Message     string             `json:"message"`
	EntityID    string             `json:"entityId,omitempty"`
	PropertyID  string             `json:"propertyId,omitempty"`
	StatementID string             `json:"statementId,omitempty"`
	ClassID     string             `json:"classId,omitempty"`
	ShapeCode   string             `json:"shapeCode,omitempty"`
}

type ValidationSummary struct {
	Errors   int `json:"errors"`
	Warnings int `json:"warnings"`
	Infos    int `json:"infos"`
}

type ValidationResult struct {
	EntityID string              `json:"entityId"`
	Findings []ValidationFinding `json:"findings"`
	Summary  ValidationSummary   `json:"summary"`
}

type ValidationReport struct {
	ID        string
	Scope     string
	EntityQID string
	Findings  []ValidationFinding
	Summary   ValidationSummary
	CreatedBy string
	CreatedAt string
}

type CreateValidationReportInput struct {
	Scope      string
	EntityQIDs []string
	Persist    bool
}
