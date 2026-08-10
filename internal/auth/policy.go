package auth

import (
	"encoding/json"
	"slices"
)

type Policy struct {
	Name     string
	Priority int
	Document PolicyDocument
}

type PolicyDocument struct {
	Effect     string              `json:"effect"`
	Operations []Operation         `json:"operations"`
	Roles      []string            `json:"roles,omitempty"`
	Subjects   []string            `json:"subjects,omitempty"`
	Resource   PolicyResource      `json:"resource,omitempty"`
	Properties []string            `json:"properties,omitempty"`
	Condition  *PolicyCondition    `json:"condition,omitempty"`
}

type PolicyResource struct {
	Type     ResourceType `json:"type"`
	PublicID string       `json:"publicId,omitempty"`
}

type PolicyCondition struct {
	SubjectAttribute string `json:"subjectAttribute"`
	EntityProperty   string `json:"entityProperty"`
}

func (d PolicyDocument) matchesSubject(subject Subject) bool {
	if len(d.Roles) == 0 && len(d.Subjects) == 0 {
		return true
	}
	for _, sid := range d.Subjects {
		if sid == subject.ID {
			return true
		}
	}
	for _, role := range d.Roles {
		if HasRole(subject, role) {
			return true
		}
	}
	return false
}

func (d PolicyDocument) matchesOperation(op Operation) bool {
	for _, o := range d.Operations {
		if o == op || o == OpManage {
			return true
		}
	}
	return false
}

func (d PolicyDocument) matchesResource(res Resource) bool {
	r := d.Resource
	if r.Type == "" || r.Type == ResourceGlobal {
		return true
	}
	switch res.Type {
	case ResourceEntity:
		if r.Type != ResourceEntity {
			return false
		}
		return r.PublicID == "" || r.PublicID == res.PublicID
	case ResourceProperty:
		if r.Type == ResourceProperty {
			return r.PublicID == "" || r.PublicID == res.PropertyID
		}
		if r.Type == ResourceEntity {
			return r.PublicID == "" || r.PublicID == res.EntityID
		}
		return false
	case ResourceStatement:
		if r.Type == ResourceEntity {
			return r.PublicID == "" || r.PublicID == res.EntityID
		}
		if r.Type == ResourceProperty {
			return r.PublicID == "" || r.PublicID == res.PropertyID
		}
		return false
	case ResourcePackage:
		if r.Type != ResourcePackage {
			return false
		}
		return r.PublicID == "" || r.PublicID == res.PublicID
	default:
		return r.Type == res.Type && (r.PublicID == "" || r.PublicID == res.PublicID)
	}
}

func (d PolicyDocument) matchesProperty(op Operation, propertyID string) bool {
	if op == OpDiscover || op == OpCreate || op == OpDelete || op == OpManage {
		return true
	}
	if len(d.Properties) == 0 {
		return true
	}
	if propertyID == "" {
		return false
	}
	return slices.Contains(d.Properties, propertyID)
}

func ParsePolicyDocument(raw []byte) (PolicyDocument, error) {
	var d PolicyDocument
	if err := json.Unmarshal(raw, &d); err != nil {
		return PolicyDocument{}, err
	}
	return d, nil
}
