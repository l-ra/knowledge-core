package datatype

import (
	"encoding/json"
	"fmt"
	"math/big"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Closed platform datatype set (TYPE-01).
type Type string

const (
	EntityReference    Type = "EntityReference"
	String             Type = "String"
	LocalizedString    Type = "LocalizedString"
	Boolean            Type = "Boolean"
	Integer            Type = "Integer"
	Decimal            Type = "Decimal"
	Date               Type = "Date"
	DateTime           Type = "DateTime"
	URI                Type = "URI"
	ExternalIdentifier Type = "ExternalIdentifier"
	Quantity           Type = "Quantity"
	Interval           Type = "Interval"
)

var allTypes = map[Type]struct{}{
	EntityReference: {}, String: {}, LocalizedString: {}, Boolean: {},
	Integer: {}, Decimal: {}, Date: {}, DateTime: {}, URI: {},
	ExternalIdentifier: {}, Quantity: {}, Interval: {},
}

func ParseType(s string) (Type, error) {
	t := Type(s)
	if _, ok := allTypes[t]; !ok {
		return "", fmt.Errorf("unknown datatype %q", s)
	}
	return t, nil
}

// Value is the API/storage union for typed statement values.
type Value struct {
	Type Type `json:"type"`

	EntityID *string           `json:"entityId,omitempty"`
	String   *string           `json:"string,omitempty"`
	LangMap  map[string]string `json:"langMap,omitempty"`
	Bool     *bool             `json:"bool,omitempty"`
	Int64    *int64            `json:"int64,omitempty"`
	Decimal  *string           `json:"decimal,omitempty"`
	Date     *string           `json:"date,omitempty"`
	DateTime *string           `json:"dateTime,omitempty"`
	URI      *string           `json:"uri,omitempty"`

	Scheme *string `json:"scheme,omitempty"`
	ExtID  *string `json:"value,omitempty"`

	QuantityValue *string `json:"quantityValue,omitempty"`
	UnitEntityID  *string `json:"unitEntityId,omitempty"`

	IntervalFrom *json.RawMessage `json:"from,omitempty"`
	IntervalTo   *json.RawMessage `json:"to,omitempty"`
}

var (
	langTagRe = regexp.MustCompile(`^[a-z]{2,3}(-[A-Za-z0-9]+)*$`)
	dateRe    = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

// Validate checks structural rules for a value of the given property datatype.
func Validate(expected Type, v Value) error {
	if v.Type == "" {
		v.Type = expected
	}
	if v.Type != expected {
		return fmt.Errorf("value type %q does not match property datatype %q", v.Type, expected)
	}
	switch expected {
	case EntityReference:
		if v.EntityID == nil || strings.TrimSpace(*v.EntityID) == "" {
			return fmt.Errorf("EntityReference requires entityId")
		}
	case String:
		if v.String == nil {
			return fmt.Errorf("String requires string")
		}
	case LocalizedString:
		if len(v.LangMap) == 0 {
			return fmt.Errorf("LocalizedString requires langMap")
		}
		for lang, text := range v.LangMap {
			if err := normalizeLang(lang); err != nil {
				return err
			}
			if strings.TrimSpace(text) == "" {
				return fmt.Errorf("LocalizedString value for %q is empty", lang)
			}
		}
	case Boolean:
		if v.Bool == nil {
			return fmt.Errorf("Boolean requires bool")
		}
	case Integer:
		if v.Int64 == nil {
			return fmt.Errorf("Integer requires int64")
		}
	case Decimal:
		if v.Decimal == nil {
			return fmt.Errorf("Decimal requires decimal")
		}
		if _, err := decimal.NewFromString(*v.Decimal); err != nil {
			return fmt.Errorf("invalid decimal: %w", err)
		}
	case Date:
		if v.Date == nil || !dateRe.MatchString(*v.Date) {
			return fmt.Errorf("Date requires YYYY-MM-DD")
		}
		if _, err := time.Parse("2006-01-02", *v.Date); err != nil {
			return fmt.Errorf("invalid date: %w", err)
		}
	case DateTime:
		if v.DateTime == nil {
			return fmt.Errorf("DateTime requires dateTime")
		}
		if _, err := time.Parse(time.RFC3339, *v.DateTime); err != nil {
			return fmt.Errorf("DateTime must be RFC3339 UTC: %w", err)
		}
	case URI:
		if v.URI == nil {
			return fmt.Errorf("URI requires uri")
		}
		u, err := url.Parse(*v.URI)
		if err != nil || u.Scheme == "" {
			return fmt.Errorf("invalid URI")
		}
	case ExternalIdentifier:
		if v.Scheme == nil || strings.TrimSpace(*v.Scheme) == "" || v.ExtID == nil || strings.TrimSpace(*v.ExtID) == "" {
			return fmt.Errorf("ExternalIdentifier requires scheme and value")
		}
	case Quantity:
		if v.QuantityValue == nil {
			return fmt.Errorf("Quantity requires quantityValue")
		}
		if _, err := decimal.NewFromString(*v.QuantityValue); err != nil {
			return fmt.Errorf("invalid quantity value: %w", err)
		}
		if v.UnitEntityID == nil || strings.TrimSpace(*v.UnitEntityID) == "" {
			return fmt.Errorf("Quantity requires unitEntityId")
		}
	case Interval:
		if v.IntervalFrom == nil && v.IntervalTo == nil {
			return fmt.Errorf("Interval requires from and/or to")
		}
	default:
		return fmt.Errorf("unsupported datatype %q", expected)
	}
	return nil
}

func normalizeLang(lang string) error {
	if !langTagRe.MatchString(lang) {
		return fmt.Errorf("invalid language tag %q", lang)
	}
	return nil
}

// RequireLabelEN enforces LABEL-01.
func RequireLabelEN(labels map[string]string) error {
	en, ok := labels["en"]
	if !ok || strings.TrimSpace(en) == "" {
		return fmt.Errorf("label.en is required")
	}
	return nil
}

// NormalizeLabels lowercases primary language subtag lightly via BCP47-ish check.
func NormalizeLabels(labels map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(labels))
	for k, v := range labels {
		if err := normalizeLang(k); err != nil {
			return nil, err
		}
		if strings.TrimSpace(v) == "" {
			return nil, fmt.Errorf("empty label for %q", k)
		}
		out[k] = v
	}
	return out, nil
}

// ParsePublicEntityID validates Q<n> form.
func ParsePublicEntityID(id string) (int64, error) {
	return parsePrefixedID("Q", id)
}

func ParsePublicPropertyID(id string) (int64, error) {
	return parsePrefixedID("P", id)
}

func ParsePublicStatementID(id string) (int64, error) {
	return parsePrefixedID("S", id)
}

func parsePrefixedID(prefix, id string) (int64, error) {
	if !strings.HasPrefix(id, prefix) {
		return 0, fmt.Errorf("expected %s prefix", prefix)
	}
	n := new(big.Int)
	if _, ok := n.SetString(id[len(prefix):], 10); !ok || n.Sign() <= 0 {
		return 0, fmt.Errorf("invalid public id %q", id)
	}
	if !n.IsInt64() {
		return 0, fmt.Errorf("public id out of range")
	}
	return n.Int64(), nil
}

func NewUUID() uuid.UUID {
	return uuid.Must(uuid.NewV7())
}
