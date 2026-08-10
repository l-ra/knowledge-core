package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/l-ra/knowledge-core/internal/auth"
)

func (e *Engine) ListPolicies(ctx context.Context) ([]auth.Policy, error) {
	if err := e.authorizeGlobal(ctx, auth.OpManage); err != nil {
		return nil, err
	}
	policies, err := e.store.ListAuthPolicies(ctx)
	if err != nil {
		return nil, mapErr(err)
	}
	return policies, nil
}

func (e *Engine) GetPolicy(ctx context.Context, name string) (*auth.Policy, error) {
	if err := e.authorizeGlobal(ctx, auth.OpManage); err != nil {
		return nil, err
	}
	policies, err := e.store.ListAuthPolicies(ctx)
	if err != nil {
		return nil, mapErr(err)
	}
	for i := range policies {
		if policies[i].Name == name {
			return &policies[i], nil
		}
	}
	return nil, ErrNotFound
}

type UpsertPolicyInput struct {
	Name     string
	Priority int
	Document auth.PolicyDocument
}

func (e *Engine) UpsertPolicy(ctx context.Context, in UpsertPolicyInput) (*auth.Policy, error) {
	if err := e.authorizeGlobal(ctx, auth.OpManage); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, fmt.Errorf("%w: name required", ErrInvalid)
	}
	if name == "bootstrap-admin" {
		return nil, fmt.Errorf("%w: bootstrap-admin is protected", ErrInvalid)
	}
	if in.Document.Effect != "allow" && in.Document.Effect != "deny" {
		return nil, fmt.Errorf("%w: effect must be allow or deny", ErrInvalid)
	}
	if len(in.Document.Operations) == 0 {
		return nil, fmt.Errorf("%w: operations required", ErrInvalid)
	}
	priority := in.Priority
	if priority == 0 {
		priority = 100
	}
	if err := e.store.UpsertAuthPolicy(ctx, name, priority, in.Document); err != nil {
		return nil, mapErr(err)
	}
	if e.auth != nil {
		if err := e.auth.Reload(ctx); err != nil {
			return nil, mapErr(err)
		}
	}
	return &auth.Policy{Name: name, Priority: priority, Document: in.Document}, nil
}

func (e *Engine) DeletePolicy(ctx context.Context, name string) error {
	if err := e.authorizeGlobal(ctx, auth.OpManage); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("%w: name required", ErrInvalid)
	}
	if name == "bootstrap-admin" {
		return fmt.Errorf("%w: bootstrap-admin is protected", ErrInvalid)
	}
	if err := e.store.DeleteAuthPolicy(ctx, name); err != nil {
		return mapErr(err)
	}
	if e.auth != nil {
		if err := e.auth.Reload(ctx); err != nil {
			return mapErr(err)
		}
	}
	return nil
}
