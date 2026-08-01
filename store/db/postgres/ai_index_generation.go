package postgres

import (
	"context"
	"strings"

	"github.com/pkg/errors"

	"github.com/usememos/memos/store"
)

func (d *DB) UpsertAIIndexGeneration(ctx context.Context, upsert *store.AIIndexGeneration) (*store.AIIndexGeneration, error) {
	fields := []string{"fingerprint", "provider_id", "provider_type", "endpoint_identity", "model", "dimensions", "state", "memo_total", "memo_indexed", "last_error", "retired_ts"}
	args := []any{upsert.Fingerprint, upsert.ProviderID, upsert.ProviderType, upsert.EndpointIdentity, upsert.Model, upsert.Dimensions, upsert.State, upsert.MemoTotal, upsert.MemoIndexed, upsert.LastError, upsert.RetiredTs}

	stmt := "INSERT INTO ai_index_generation (" + strings.Join(fields, ", ") + ") VALUES (" + placeholders(len(args)) + ")" +
		" ON CONFLICT (fingerprint) DO UPDATE SET" +
		" provider_id = EXCLUDED.provider_id," +
		" provider_type = EXCLUDED.provider_type," +
		" endpoint_identity = EXCLUDED.endpoint_identity," +
		" model = EXCLUDED.model," +
		" dimensions = EXCLUDED.dimensions," +
		" state = EXCLUDED.state," +
		" memo_total = EXCLUDED.memo_total," +
		" memo_indexed = EXCLUDED.memo_indexed," +
		" last_error = EXCLUDED.last_error," +
		" retired_ts = EXCLUDED.retired_ts," +
		" updated_ts = EXTRACT(EPOCH FROM NOW())" +
		" RETURNING id, created_ts, updated_ts"
	if err := d.db.QueryRowContext(ctx, stmt, args...).Scan(
		&upsert.ID,
		&upsert.CreatedTs,
		&upsert.UpdatedTs,
	); err != nil {
		return nil, err
	}

	return upsert, nil
}

func (d *DB) ListAIIndexGenerations(ctx context.Context, find *store.FindAIIndexGeneration) ([]*store.AIIndexGeneration, error) {
	where, args := []string{"1 = 1"}, []any{}

	if find.ID != nil {
		where, args = append(where, "id = "+placeholder(len(args)+1)), append(args, *find.ID)
	}
	if find.Fingerprint != nil {
		where, args = append(where, "fingerprint = "+placeholder(len(args)+1)), append(args, *find.Fingerprint)
	}
	if find.State != nil {
		where, args = append(where, "state = "+placeholder(len(args)+1)), append(args, *find.State)
	}

	query := "SELECT id, fingerprint, provider_id, provider_type, endpoint_identity, model, dimensions, state, memo_total, memo_indexed, last_error, retired_ts, created_ts, updated_ts FROM ai_index_generation WHERE " + strings.Join(where, " AND ") + " ORDER BY id ASC"
	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []*store.AIIndexGeneration{}
	for rows.Next() {
		generation := &store.AIIndexGeneration{}
		if err := rows.Scan(
			&generation.ID,
			&generation.Fingerprint,
			&generation.ProviderID,
			&generation.ProviderType,
			&generation.EndpointIdentity,
			&generation.Model,
			&generation.Dimensions,
			&generation.State,
			&generation.MemoTotal,
			&generation.MemoIndexed,
			&generation.LastError,
			&generation.RetiredTs,
			&generation.CreatedTs,
			&generation.UpdatedTs,
		); err != nil {
			return nil, err
		}
		list = append(list, generation)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return list, nil
}

func (d *DB) PromoteAIIndexGeneration(ctx context.Context, promote *store.AIIndexGenerationPromotion) (bool, error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return false, errors.Wrap(err, "failed to begin generation promotion transaction")
	}
	defer func() {
		_ = tx.Rollback()
	}()

	// The conditional promote: the generation must still be building and still
	// match the desired embedding assignment, or the cutover does not happen
	// and no state changes.
	result, err := tx.ExecContext(ctx,
		"UPDATE ai_index_generation SET state = $1, retired_ts = 0, updated_ts = EXTRACT(EPOCH FROM NOW()) WHERE id = $2 AND state = $3 AND fingerprint = $4",
		store.AIIndexGenerationActive, promote.ID, store.AIIndexGenerationBuilding, promote.Fingerprint,
	)
	if err != nil {
		return false, errors.Wrap(err, "failed to promote ai index generation")
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, errors.Wrap(err, "failed to check promoted ai index generation")
	}
	if affected == 0 {
		return false, nil
	}

	// The former active generation retires in the same transaction, so queries
	// never observe zero or two active generations partway through the cutover.
	if _, err := tx.ExecContext(ctx,
		"UPDATE ai_index_generation SET state = $1, retired_ts = $2, updated_ts = EXTRACT(EPOCH FROM NOW()) WHERE state = $3 AND id <> $4",
		store.AIIndexGenerationRetired, promote.RetiredTs, store.AIIndexGenerationActive, promote.ID,
	); err != nil {
		return false, errors.Wrap(err, "failed to retire former ai index generation")
	}

	if err := tx.Commit(); err != nil {
		return false, errors.Wrap(err, "failed to commit generation promotion transaction")
	}
	return true, nil
}

func (d *DB) DeleteAIIndexGeneration(ctx context.Context, delete *store.DeleteAIIndexGeneration) error {
	where, args := []string{"1 = 1"}, []any{}
	if delete.ID != nil {
		where, args = append(where, "id = "+placeholder(len(args)+1)), append(args, *delete.ID)
	}

	stmt := "DELETE FROM ai_index_generation WHERE " + strings.Join(where, " AND ")
	if _, err := d.db.ExecContext(ctx, stmt, args...); err != nil {
		return errors.Wrap(err, "failed to delete ai index generation")
	}
	return nil
}
