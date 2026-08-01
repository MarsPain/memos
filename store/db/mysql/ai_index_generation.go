package mysql

import (
	"context"
	"strings"

	"github.com/pkg/errors"

	"github.com/usememos/memos/store"
)

func (d *DB) UpsertAIIndexGeneration(ctx context.Context, upsert *store.AIIndexGeneration) (*store.AIIndexGeneration, error) {
	fields := []string{"`fingerprint`", "`provider_id`", "`provider_type`", "`endpoint_identity`", "`model`", "`dimensions`", "`state`", "`memo_total`", "`memo_indexed`", "`last_error`", "`retired_ts`"}
	placeholder := []string{"?", "?", "?", "?", "?", "?", "?", "?", "?", "?", "?"}
	args := []any{upsert.Fingerprint, upsert.ProviderID, upsert.ProviderType, upsert.EndpointIdentity, upsert.Model, upsert.Dimensions, upsert.State, upsert.MemoTotal, upsert.MemoIndexed, upsert.LastError, upsert.RetiredTs}

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
		" `retired_ts` = VALUES(`retired_ts`)," +
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

	query := "SELECT `id`, `fingerprint`, `provider_id`, `provider_type`, `endpoint_identity`, `model`, `dimensions`, `state`, `memo_total`, `memo_indexed`, `last_error`, `retired_ts`, `created_ts`, `updated_ts` FROM `ai_index_generation` WHERE " + strings.Join(where, " AND ") + " ORDER BY `id` ASC"
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
		"UPDATE `ai_index_generation` SET `state` = ?, `retired_ts` = 0, `updated_ts` = UNIX_TIMESTAMP() WHERE `id` = ? AND `state` = ? AND `fingerprint` = ?",
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
		"UPDATE `ai_index_generation` SET `state` = ?, `retired_ts` = ?, `updated_ts` = UNIX_TIMESTAMP() WHERE `state` = ? AND `id` <> ?",
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
		where, args = append(where, "`id` = ?"), append(args, *delete.ID)
	}

	stmt := "DELETE FROM `ai_index_generation` WHERE " + strings.Join(where, " AND ")
	if _, err := d.db.ExecContext(ctx, stmt, args...); err != nil {
		return errors.Wrap(err, "failed to delete ai index generation")
	}
	return nil
}
