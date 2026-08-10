package auth

import "context"

type Operation string

const (
	OpDiscover Operation = "discover"
	OpRead     Operation = "read"
	OpCreate   Operation = "create"
	OpUpdate   Operation = "update"
	OpDelete   Operation = "delete"
	OpManage   Operation = "manage"
)

type ResourceType string

const (
	ResourceGlobal   ResourceType = "global"
	ResourceEntity   ResourceType = "entity"
	ResourceProperty ResourceType = "property"
	ResourceStatement ResourceType = "statement"
	ResourcePackage  ResourceType = "package"
)

type Resource struct {
	Type       ResourceType
	PublicID   string
	EntityID   string
	PropertyID string
}

type Subject struct {
	ID         string
	Roles      []string
	Attributes map[string]string
}

type ctxKey struct{}

func WithSubject(ctx context.Context, s Subject) context.Context {
	return context.WithValue(ctx, ctxKey{}, s)
}

func SubjectFromContext(ctx context.Context) (Subject, bool) {
	s, ok := ctx.Value(ctxKey{}).(Subject)
	return s, ok && s.ID != ""
}

func HasRole(s Subject, role string) bool {
	for _, r := range s.Roles {
		if r == role {
			return true
		}
	}
	return false
}
