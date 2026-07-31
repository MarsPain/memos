package mysql

import (
	"context"
	"strings"

	"github.com/pkg/errors"

	"github.com/usememos/memos/store"
)

func (d *DB) UpsertAIIndexGeneration(ctx context.Context, upsert *store.AIIndexGeneration) (*store.AIIndexGeneration, error) {
	fields := []string{"`fingerprint`", "`provider_id`", "`provider_type`", "`endpoint_identity`", "`model`", "`dimensions`", "`state`", "`memo_total`", "`memo_indexed`", "`last_error`"}
	placeholder := []string{"?", "?", "?", "?", "?", "?", "?", "?", "?", "?"}
	args := []any{upsert.Fingerprint, upsert.ProviderID, upsert.ProviderType, upsert.EndpointIdentity, upsert.Model, upsert.Dimensions, upsert.State, upsert.MemoTotal, upsert.MemoIndexed, upsert.LastError}

	stmt := "INSERT INTO `ai_index_generation` (" + strings.Join(fields, ", ") + ") VALUES (" + strings.Join(placeholder, ", ") + ")" +
		" ON DUPLICATE KEY UPDATE" +
		" `provider_id` = VALUES(`provider_id`)," +
		" `provider_type` = VALUES(`provider_type`)," +
		" `endpoint_identity` = VALUES(`endpoint_identity`)," +
		" `model` = VALUES(`model`)," +
		" `dimensions` = VALUES(`dimensions`)," +
		" `state` = VALUES(`state`)," +
		" `memo_total` = VALUES(`memo_total`)," +
		" `memo_indexed` = VALUES(`memo_indexed`)," +
		" `last_error` = VALUES(`last_error`)," +
		" `updated_ts` = UNIX_TIMESTAMP()"
	if _, err := d.db.ExecContext(ctx, stmt, args...); err != nil {
		return nil, err
	}

	list, err := d.ListAIIndexGenerations(ctx, &store.FindAIIndexGeneration{Fingerprint: &upsert.Fingerprint})
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, errors.Errorf("failed to upsert ai index generation")
	}
	return list[0], nil
}

func (d *DB) ListAIIndexGenerations(ctx context.Context, find *store.FindAIIndexGeneration) ([]*store.AIIndexGeneration, error) {
	where, args := []string{"1 = 1"}, []any{}

	if find.ID != nil {
		where, args = append(where, "`id` = ?"), append(args, *find.ID)
	}
	if find.Fingerprint != nil {
		where, args = append(where, "`fingerprint` = ?"), append(args, *find.Fingerprint)
	}
	if find.State != nil {
		where, args = append(where, "`state` = ?"), append(args, *find.State)
	}

	query := "SELECT `id`, `fingerprint`, `provider_id`, `provider_type`, `endpoint_identity`, `model`, `dimensions`, `state`, `memo_total`, `memo_indexed`, `last_error`, `created_ts`, `updated_ts` FROM `ai_index_generation` WHERE " + strings.Join(where, " AND ") + " ORDER BY `id` ASC"
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
		where, args = append(where, "`id` = ?"), append(args, *delete.ID)
	}

	stmt := "DELETE FROM `ai_index_generation` WHERE " + strings.Join(where, " AND ")
	if _, err := d.db.ExecContext(ctx, stmt, args...); err != nil {
		return errors.Wrap(err, "failed to delete ai index generation")
	}
	return nil
}
