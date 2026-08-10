package domain

import (
	"time"

	"github.com/google/uuid"
	"github.com/rasekl/knowledge-core/internal/datatype"
)

type EntityStatus string

const (
	EntityActive     EntityStatus = "active"
	EntityDeprecated EntityStatus = "deprecated"
	EntityRedirected EntityStatus = "redirected"
	EntityDeleted    EntityStatus = "deleted"
)

type PropertyStatus string

const (
	PropertyActive     PropertyStatus = "active"
	PropertyDeprecated PropertyStatus = "deprecated"
	PropertyDeleted    PropertyStatus = "deleted"
)

type StatementStatus string

const (
	StatementActive     StatementStatus = "active"
	StatementDeprecated StatementStatus = "deprecated"
	StatementDeleted    StatementStatus = "deleted"
)

type Entity struct {
	ID           uuid.UUID
	PublicID     string
	Status       EntityStatus
	Labels       map[string]string
	Descriptions map[string]string
	RevisionNo   int
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Property struct {
	ID           uuid.UUID
	PublicID     string
	Datatype     datatype.Type
	Status       PropertyStatus
	Labels       map[string]string
	Descriptions map[string]string
	RevisionNo   int
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Statement struct {
	ID           uuid.UUID
	PublicID     string
	SubjectID    uuid.UUID
	SubjectQID   string
	PropertyID   uuid.UUID
	PropertyPID  string
	Status       StatementStatus
	Value        datatype.Value
	RevisionNo   int
	Qualifiers   []Qualifier
	ReferenceIDs []string
	ValidFrom    *time.Time
	ValidTo      *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Qualifier struct {
	PropertyPID string
	PropertyID  uuid.UUID
	Value       datatype.Value
}

type Reference struct {
	ID        uuid.UUID
	PublicID  string
	Fields    map[string]any
	CreatedAt time.Time
}

type QualifierInput struct {
	Property string         `json:"property"`
	Value    datatype.Value `json:"value"`
}

type WriteMeta struct {
	Actor          string
	IdempotencyKey string
	CorrelationID  string
	OperationType  string
	RequestHash    string
}

type ChangeSet struct {
	ID              uuid.UUID
	PublicID        string
	Actor           string
	OperationType   string
	CommittedAt     time.Time
	IdempotencyKey  string
	CorrelationID   string
	Items           []ChangeSetItem
}

type ChangeSetItem struct {
	ObjectType string
	ObjectID   uuid.UUID
	PublicID   string
	Op         string
}

type EntityRevision struct {
	RevisionNo  int
	Status      EntityStatus
	Labels      map[string]string
	Descriptions map[string]string
	Actor       string
	ChangeSetID *uuid.UUID
	CreatedAt   time.Time
}

type StatementRevision struct {
	RevisionNo   int
	Status       StatementStatus
	Value        datatype.Value
	Qualifiers   []Qualifier
	ReferenceIDs []string
	ValidFrom    *time.Time
	ValidTo      *time.Time
	Actor        string
	ChangeSetID  *uuid.UUID
	CreatedAt    time.Time
}

type WriteResult[T any] struct {
	Value      T
	ChangeSet  *ChangeSet
	Replay     bool
	ResponseRaw []byte
}

type CreateEntityInput struct {
	PackageCode  string
	Labels       map[string]string
	Descriptions map[string]string
}

type UpdateEntityInput struct {
	Labels           map[string]string
	Descriptions     map[string]string
	ExpectedRevision int
}

type CreatePropertyInput struct {
	PackageCode  string
	Datatype     datatype.Type
	Labels       map[string]string
	Descriptions map[string]string
}

type CreateStatementInput struct {
	PackageCode      string
	SubjectPublicID  string
	PropertyPublicID string
	Value            datatype.Value
	Qualifiers       []QualifierInput
	ReferenceIDs     []string
	ValidFrom        *time.Time
	ValidTo          *time.Time
}

type ReviseStatementInput struct {
	Value            *datatype.Value
	Qualifiers       []QualifierInput
	ReplaceQualifiers bool
	ReferenceIDs     []string
	ReplaceReferences bool
	ValidFrom        *time.Time
	ValidTo          *time.Time
	ReplaceValidTime bool
	ExpectedRevision int
}

type CreateReferenceInput struct {
	Fields map[string]any
}

type ChangeOperation struct {
	Op               string          `json:"op"`
	Statement        string          `json:"statement,omitempty"`
	Entity           string          `json:"entity,omitempty"`
	Value            datatype.Value  `json:"value,omitempty"`
	Labels           map[string]string `json:"labels,omitempty"`
	Descriptions     map[string]string `json:"descriptions,omitempty"`
	ExpectedRevision int             `json:"expectedRevision,omitempty"`
}

type ApplyChangeSetInput struct {
	OperationType string
	Comment       string
	Operations    []ChangeOperation
}

type PackageLifecycle string

const (
	PackageReleased   PackageLifecycle = "released"
	PackageContinuous PackageLifecycle = "continuous"
)

type Package struct {
	ID           uuid.UUID
	Code         string
	Lifecycle    PackageLifecycle
	Labels       map[string]string
	Dependencies []PackageDependency
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type PackageDependency struct {
	DependsOnCode string
	VersionRange  string
}

type Release struct {
	ID           uuid.UUID
	PackageCode  string
	Version      string
	PublishedAt  time.Time
	Dependencies []ReleaseDependency
	Objects      []ReleaseObject
}

type ReleaseDependency struct {
	DependencyCode    string
	DependencyVersion string
}

type ReleaseObject struct {
	ObjectType     string
	ObjectPublicID string
	RevisionNo     int
}

type Bundle struct {
	Manifest   BundleManifest    `json:"manifest"`
	Releases   []BundleManifest  `json:"releases,omitempty"`
	Entities   []BundleEntity    `json:"entities,omitempty"`
	Properties []BundleProperty  `json:"properties,omitempty"`
	Statements []BundleStatement `json:"statements,omitempty"`
	References []BundleReference `json:"references,omitempty"`
}

type BundleManifest struct {
	FormatVersion int                    `json:"formatVersion"`
	Package       string                 `json:"package"`
	Version       string                 `json:"version"`
	PublishedAt   string                 `json:"publishedAt"`
	Dependencies  []ReleaseDependency    `json:"dependencies"`
	ObjectIndex   []ReleaseObject        `json:"objectIndex"`
}

type BundleEntity struct {
	PublicID     string            `json:"id"`
	PackageCode  string            `json:"packageCode,omitempty"`
	RevisionNo   int               `json:"revisionNo"`
	Status       EntityStatus      `json:"status"`
	Labels       map[string]string `json:"labels"`
	Descriptions map[string]string `json:"descriptions"`
}

type BundleProperty struct {
	PublicID     string            `json:"id"`
	PackageCode  string            `json:"packageCode,omitempty"`
	RevisionNo   int               `json:"revisionNo"`
	Datatype     datatype.Type     `json:"datatype"`
	Status       PropertyStatus    `json:"status"`
	Labels       map[string]string `json:"labels"`
	Descriptions map[string]string `json:"descriptions"`
}

type BundleStatement struct {
	PublicID     string           `json:"id"`
	PackageCode  string           `json:"packageCode,omitempty"`
	RevisionNo   int              `json:"revisionNo"`
	Subject      string           `json:"subject"`
	Property     string           `json:"property"`
	Status       StatementStatus  `json:"status"`
	Value        datatype.Value   `json:"value"`
	Qualifiers   []QualifierInput `json:"qualifiers,omitempty"`
	ReferenceIDs []string         `json:"referenceIds,omitempty"`
}

type BundleReference struct {
	PublicID string         `json:"id"`
	Fields   map[string]any `json:"fields"`
}

type CreatePackageInput struct {
	Code         string
	Lifecycle    PackageLifecycle
	Labels       map[string]string
	Dependencies []PackageDependency
}

type PublishReleaseInput struct {
	Version string
}
