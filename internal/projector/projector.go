package projector

import (
	"context"

	"github.com/google/uuid"
	"github.com/l-ra/knowledge-core/internal/domain"
)

type OutboxStore interface {
	ListPendingOutboxEvents(ctx context.Context, limit int) ([]domain.OutboxEvent, error)
	ApplyOutboxToSearchProjection(ctx context.Context, ev domain.OutboxEvent) error
	ApplyOutboxToRDFProjection(ctx context.Context, ev domain.OutboxEvent) error
	MarkOutboxPublished(ctx context.Context, ids []uuid.UUID) error
}

func ProcessPending(ctx context.Context, store OutboxStore, limit int) (int, error) {
	events, err := store.ListPendingOutboxEvents(ctx, limit)
	if err != nil {
		return 0, err
	}
	if len(events) == 0 {
		return 0, nil
	}
	ids := make([]uuid.UUID, 0, len(events))
	for _, ev := range events {
		if err := store.ApplyOutboxToSearchProjection(ctx, ev); err != nil {
			return 0, err
		}
		if err := store.ApplyOutboxToRDFProjection(ctx, ev); err != nil {
			return 0, err
		}
		id, err := uuid.Parse(ev.ID)
		if err != nil {
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := store.MarkOutboxPublished(ctx, ids); err != nil {
		return 0, err
	}
	return len(events), nil
}
