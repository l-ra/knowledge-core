package domain

import "github.com/rasekl/knowledge-core/internal/datatype"

type LensCardinality string

const (
	CardinalityOne       LensCardinality = "one"
	CardinalityZeroOrOne LensCardinality = "zeroOrOne"
	CardinalityMany      LensCardinality = "many"
)

type LensDefinition struct {
	ID        string
	Code      string
	Version   int
	Labels    map[string]string
	Document  LensDocument
	CreatedAt string
	UpdatedAt string
}

type LensDocument struct {
	EntitySelector *LensEntitySelector    `json:"entitySelector,omitempty"`
	Key            LensKey                `json:"key"`
	Fields         map[string]LensField   `json:"fields"`
}

type LensEntitySelector struct {
	TypeEntity string `json:"typeEntity,omitempty"`
}

type LensKey struct {
	Property string `json:"property"`
}

type LensField struct {
	Property   string          `json:"property"`
	Type       datatype.Type   `json:"type"`
	Cardinality LensCardinality `json:"cardinality"`
	NestedLens string          `json:"lens,omitempty"`
}

type CreateLensInput struct {
	Code     string
	Labels   map[string]string
	Document LensDocument
}

type LensPatchOperation struct {
	Op    string         `json:"op"`
	Field string         `json:"field"`
	Value datatype.Value `json:"value,omitempty"`
}

type LensPatchInput struct {
	Operations []LensPatchOperation `json:"operations"`
}
