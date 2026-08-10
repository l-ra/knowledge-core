package auth

import (
	"context"
	"sync"
)

type PolicyLoader interface {
	ListAuthPolicies(ctx context.Context) ([]Policy, error)
}

type Engine struct {
	loader          PolicyLoader
	bootstrapAdmin  string
	mu              sync.RWMutex
	policies        []Policy
}

func NewEngine(loader PolicyLoader, bootstrapAdmin string) *Engine {
	return &Engine{loader: loader, bootstrapAdmin: bootstrapAdmin}
}

func (e *Engine) Reload(ctx context.Context) error {
	policies, err := e.loader.ListAuthPolicies(ctx)
	if err != nil {
		return err
	}
	e.mu.Lock()
	e.policies = policies
	e.mu.Unlock()
	return nil
}

func (e *Engine) Allow(ctx context.Context, subject Subject, op Operation, res Resource, entityAttrs map[string]string) (bool, error) {
	if err := e.ensureLoaded(ctx); err != nil {
		return false, err
	}
	if e.isBootstrapAdmin(subject) || HasRole(subject, "admin") {
		return true, nil
	}
	if op != OpManage && e.checkPolicies(subject, OpManage, res, entityAttrs, true) {
		return true, nil
	}
	if e.checkPolicies(subject, op, res, entityAttrs, false) {
		return true, nil
	}
	return false, nil
}

func (e *Engine) isBootstrapAdmin(subject Subject) bool {
	return e.bootstrapAdmin != "" && subject.ID == e.bootstrapAdmin
}

func (e *Engine) ensureLoaded(ctx context.Context) error {
	e.mu.RLock()
	loaded := len(e.policies) > 0
	e.mu.RUnlock()
	if loaded {
		return nil
	}
	return e.Reload(ctx)
}

func (e *Engine) checkPolicies(subject Subject, op Operation, res Resource, entityAttrs map[string]string, manageOnly bool) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var allows, denies []PolicyDocument
	for _, p := range e.policies {
		d := p.Document
		if manageOnly && !d.matchesOperation(OpManage) {
			continue
		}
		if !d.matchesSubject(subject) || !d.matchesOperation(op) || !d.matchesResource(res) {
			continue
		}
		if !d.matchesProperty(op, res.PropertyID) {
			continue
		}
		if d.Condition != nil && !matchCondition(d.Condition, subject, entityAttrs) {
			continue
		}
		switch d.Effect {
		case "deny":
			denies = append(denies, d)
		case "allow":
			allows = append(allows, d)
		}
	}
	if len(denies) > 0 {
		return false
	}
	return len(allows) > 0
}

func matchCondition(c *PolicyCondition, subject Subject, entityAttrs map[string]string) bool {
	if c == nil {
		return true
	}
	subVal, ok := subject.Attributes[c.SubjectAttribute]
	if !ok {
		return false
	}
	entVal, ok := entityAttrs[c.EntityProperty]
	return ok && subVal == entVal
}
