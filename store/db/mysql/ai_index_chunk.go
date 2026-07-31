package mysql

import (
	"context"
	"fmt"
	"strings"

	"github.com/pkg/errors"

	"github.com/usememos/memos/store"
)

func (d *DB) UpsertAIIndexChunk(ctx context.Context, upsert *store.AIIndexChunk) (*store.AIIndexChunk, error) {
	fields := []string{"`generation_id`", "`memo_id`", "`memo_revision`", "`chunk_ordinal`", "`content_start`", "`content_end`", "`source_start`", "`source_end`", "`vector`", "`dimensions`"}
	placeholder := []string{"?", "?", "?", "?", "?", "?", "?", "?", "?", "?"}
	args := []any{upsert.GenerationID, upsert.MemoID, upsert.MemoRevision, upsert.ChunkOrdinal, upsert.ContentStart, upsert.ContentEnd, upsert.SourceStart, upsert.SourceEnd, upsert.Vector, upsert.Dimensions}

	stmt := "INSERT INTO `ai_index_chunk` (" + strings.Join(fields, ", ") + ") VALUES (" + strings.Join(placeholder, ", ") + ")" +
		" ON DUPLICATE KEY UPDATE" +
		" `content_start` = VALUES(`content_start`)," +
		" `content_end` = VALUES(`content_end`)," +
		" `source_start` = VALUES(`source_start`)," +
		" `source_end` = VALUES(`source_end`)," +
		" `vector` = VALUES(`vector`)," +
		" `dimensions` = VALUES(`dimensions`)," +
		" `indexed_ts` = UNIX_TIMESTAMP()"
	if _, err := d.db.ExecContext(ctx, stmt, args...); err != nil {
		return nil, err
	}

	// MySQL has no RETURNING, so re-select the row by its conflict key.
	chunk := &store.AIIndexChunk{}
	if err := d.db.QueryRowContext(ctx,
		"SELECT `id`, `generation_id`, `memo_id`, `memo_revision`, `chunk_ordinal`, `content_start`, `content_end`, `source_start`, `source_end`, `vector`, `dimensions`, `indexed_ts` FROM `ai_index_chunk` WHERE `generation_id` = ? AND `memo_id` = ? AND `memo_revision` = ? AND `chunk_ordinal` = ?",
		upsert.GenerationID, upsert.MemoID, upsert.MemoRevision, upsert.ChunkOrdinal,
	).Scan(
		&chunk.ID,
		&chunk.GenerationID,
		&chunk.MemoID,
		&chunk.MemoRevision,
		&chunk.ChunkOrdinal,
		&chunk.ContentStart,
		&chunk.ContentEnd,
		&chunk.SourceStart,
		&chunk.SourceEnd,
		&chunk.Vector,
		&chunk.Dimensions,
		&chunk.IndexedTs,
	); err != nil {
		return nil, errors.Wrap(err, "failed to upsert ai index chunk")
	}
	return chunk, nil
}

func (d *DB) ListAIIndexChunks(ctx context.Context, find *store.FindAIIndexChunk) ([]*store.AIIndexChunk, error) {
	where, args := []string{"1 = 1"}, []any{}

	if find.ID != nil {
		where, args = append(where, "`id` = ?"), append(args, *find.ID)
	}
	if find.GenerationID != nil {
		where, args = append(where, "`generation_id` = ?"), append(args, *find.GenerationID)
	}
	if find.MemoID != nil {
		where, args = append(where, "`memo_id` = ?"), append(args, *find.MemoID)
	}
	if find.IDGreaterThan != nil {
		where, args = append(where, "`id` > ?"), append(args, *find.IDGreaterThan)
	}

	query := "SELECT `id`, `generation_id`, `memo_id`, `memo_revision`, `chunk_ordinal`, `content_start`, `content_end`, `source_start`, `source_end`, `vector`, `dimensions`, `indexed_ts` FROM `ai_index_chunk` WHERE " + strings.Join(where, " AND ") + " ORDER BY `id` ASC"
	if find.Limit != nil {
		query = fmt.Sprintf("%s LIMIT %d", query, *find.Limit)
	}
	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []*store.AIIndexChunk{}
	for rows.Next() {
		chunk := &store.AIIndexChunk{}
		if err := rows.Scan(
			&chunk.ID,
			&chunk.GenerationID,
			&chunk.MemoID,
			&chunk.MemoRevision,
			&chunk.ChunkOrdinal,
			&chunk.ContentStart,
			&chunk.ContentEnd,
			&chunk.SourceStart,
			&chunk.SourceEnd,
			&chunk.Vector,
			&chunk.Dimensions,
			&chunk.IndexedTs,
		); err != nil {
			return nil, err
		}
		list = append(list, chunk)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return list, nil
}

func (d *DB) DeleteAIIndexChunk(ctx context.Context, delete *store.DeleteAIIndexChunk) error {
	where, args := []string{"1 = 1"}, []any{}
	if delete.ID != nil {
		where, args = append(where, "`id` = ?"), append(args, *delete.ID)
	}
	if delete.GenerationID != nil {
		where, args = append(where, "`generation_id` = ?"), append(args, *delete.GenerationID)
	}
	if delete.MemoID != nil {
		where, args = append(where, "`memo_id` = ?"), append(args, *delete.MemoID)
	}

	stmt := "DELETE FROM `ai_index_chunk` WHERE " + strings.Join(where, " AND ")
	if _, err := d.db.ExecContext(ctx, stmt, args...); err != nil {
		return errors.Wrap(err, "failed to delete ai index chunk")
	}
	return nil
}
