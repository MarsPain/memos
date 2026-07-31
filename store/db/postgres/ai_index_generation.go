package postgres

import (
	"context"
	"strings"

	"github.com/pkg/errors"

	"github.com/usememos/memos/store"
)

func (d *DB) UpsertAIIndexGeneration(ctx context.Context, upsert *store.AIIndexGeneration) (*store.AIIndexGeneration, error) {
	fields := []string{"fingerprint", "provider_id", "provider_type", "endpoint_identity", "model", "dimensions", "state", "memo_total", "memo_indexed", "last_error"}
	args := []any{upsert.Fingerprint, upsert.ProviderID, upsert.ProviderType, upsert.EndpointIdentity, upsert.Model, upsert.Dimensions, upsert.State, upsert.MemoTotal, upsert.MemoIndexed, upsert.LastError}

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

	query := "SELECT id, fingerprint, provider_id, provider_type, endpoint_identity, model, dimensions, state, memo_total, memo_indexed, last_error, created_ts, updated_ts FROM ai_index_generation WHERE " + strings.Join(where, " AND ") + " ORDER BY id ASC"
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
