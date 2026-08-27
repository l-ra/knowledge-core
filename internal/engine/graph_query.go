package engine

import (
	"context"
	"fmt"

	"github.com/l-ra/knowledge-core/internal/auth"
	"github.com/l-ra/knowledge-core/internal/datatype"
	"github.com/l-ra/knowledge-core/internal/domain"
)

func (e *Engine) ListIncomingStatements(ctx context.Context, qid, propertyPID string) ([]domain.Statement, error) {
	if _, err := e.GetEntity(ctx, qid); err != nil {
		return nil, err
	}
	list, err := e.store.ListStatementsByObject(ctx, qid, propertyPID)
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]domain.Statement, 0, len(list))
	for i := range list {
		if err := e.authorizeEntity(ctx, auth.OpDiscover, list[i].SubjectQID); err != nil {
			continue
		}
		st, err := e.presentStatement(ctx, &list[i])
		if err != nil {
			return nil, err
		}
		filtered, err := e.filterStatements(ctx, st.SubjectQID, []domain.Statement{*st})
		if err != nil || len(filtered) == 0 {
			continue
		}
		out = append(out, filtered[0])
	}
	return out, nil
}

func (e *Engine) GetEntityGraph(ctx context.Context, qid string, depth int) (*domain.EntityGraph, error) {
	if depth <= 0 {
		depth = 1
	}
	if depth > 2 {
		depth = 2
	}
	ent, err := e.GetEntity(ctx, qid)
	if err != nil {
		return nil, err
	}
	outSt, err := e.ListEntityStatements(ctx, qid, "")
	if err != nil {
		return nil, err
	}
	inSt, err := e.ListIncomingStatements(ctx, qid, "")
	if err != nil {
		return nil, err
	}
	g := &domain.EntityGraph{Entity: *ent, Outgoing: outSt, Incoming: inSt}
	seen := map[string]bool{qid: true}
	addNeighbor := func(id string) {
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		n, err := e.GetEntity(ctx, id)
		if err != nil {
			return
		}
		g.Neighbors = append(g.Neighbors, *n)
	}
	collectRefs := func(list []domain.Statement) {
		for _, st := range list {
			addNeighbor(st.SubjectQID)
			if st.Value.Type == datatype.EntityReference && st.Value.EntityID != nil {
				addNeighbor(*st.Value.EntityID)
			}
		}
	}
	collectRefs(outSt)
	collectRefs(inSt)
	if depth >= 2 {
		hop := append([]domain.Entity{}, g.Neighbors...)
		for _, n := range hop {
			o, err := e.ListEntityStatements(ctx, n.PublicID, "")
			if err != nil {
				continue
			}
			i, err := e.ListIncomingStatements(ctx, n.PublicID, "")
			if err != nil {
				continue
			}
			collectRefs(o)
			collectRefs(i)
		}
	}
	return g, nil
}

func (e *Engine) UpdateProperty(ctx context.Context, meta domain.WriteMeta, pid string, in domain.UpdatePropertyInput) (*domain.WriteResult[domain.Property], error) {
	if _, err := datatype.ParsePublicPropertyID(pid); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if err := e.authorizeGlobal(ctx, auth.OpUpdate); err != nil {
		return nil, err
	}
	res, err := e.store.UpdateProperty(ctx, meta, pid, in)
	if err != nil {
		return nil, mapErr(err)
	}
	return res, nil
}

func (e *Engine) MoveEntity(ctx context.Context, meta domain.WriteMeta, qid string, in domain.MoveEntityInput) (*domain.WriteResult[domain.Entity], error) {
	if _, _, err := datatype.ParsePublicGraphID(qid); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if err := e.authorizeEntity(ctx, auth.OpUpdate, qid); err != nil {
		return nil, err
	}
	if err := e.authorizePackage(ctx, auth.OpUpdate, in.PackageCode); err != nil {
		return nil, err
	}
	res, err := e.store.MoveEntity(ctx, meta, qid, in)
	if err != nil {
		return nil, mapErr(err)
	}
	return res, nil
}
