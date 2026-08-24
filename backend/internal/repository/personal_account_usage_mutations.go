package repository

import (
	"context"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// BatchUpdateLastUsed replaces PostgreSQL CASE/ANY/timestamptz batching with a
// small Ent transaction. Personal Edition only has a handful of upstream
// accounts, so correctness and SQLite portability matter more than a giant
// single-statement batch optimized for SaaS-scale pools.
func (r *personalAccountRepository) BatchUpdateLastUsed(ctx context.Context, updates map[int64]time.Time) error {
	if len(updates) == 0 {
		return nil
	}

	tx, err := r.base.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	for id, ts := range updates {
		if id <= 0 {
			continue
		}
		if _, err := tx.Client().Account.UpdateOneID(id).SetLastUsedAt(ts).Save(ctx); err != nil {
			return err
		}
	}

	lastUsedPayload := make(map[string]int64, len(updates))
	for id, ts := range updates {
		if id > 0 {
			lastUsedPayload[strconv.FormatInt(id, 10)] = ts.Unix()
		}
	}
	if len(lastUsedPayload) > 0 {
		payload := map[string]any{"last_used": lastUsedPayload}
		if err := enqueueSchedulerOutbox(ctx, tx.Client(), service.SchedulerOutboxEventAccountLastUsed, nil, nil, payload); err != nil {
			return err
		}
	}
	return tx.Commit()
}
