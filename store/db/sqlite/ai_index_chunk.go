package sqlite

import (
	"context"
	"fmt"
	"strings"

	"github.com/pkg/errors"

	"github.com/usememos/memos/store"
)

func (d *DB) UpsertAIIndexChunk(ctx context.Context, upsert *store.AIIndexChunk) (*store.AIIndexChunk, error) {
	fields := []string{"`generation_id`", "`memo_id`", "`memo_revision`", "`chunk_ordinal`", "`content_start`", "`content_end`", "`source_start`", "`source_end`", "`vector`", "`dimensions`", "`content_hash`"}
	placeholder := []string{"?", "?", "?", "?", "?", "?", "?", "?", "?", "?", "?"}
	args := []any{upsert.GenerationID, upsert.MemoID, upsert.MemoRevision, upsert.ChunkOrdinal, upsert.ContentStart, upsert.ContentEnd, upsert.SourceStart, upsert.SourceEnd, upsert.Vector, upsert.Dimensions, upsert.ContentHash}

	stmt := "INSERT INTO `ai_index_chunk` (" + strings.Join(fields, ", ") + ") VALUES (" + strings.Join(placeholder, ", ") + ")" +
		" ON CONFLICT(`generation_id`, `memo_id`, `memo_revision`, `chunk_ordinal`) DO UPDATE SET" +
		" `content_start` = `excluded`.`content_start`," +
		" `content_end` = `excluded`.`content_end`," +
		" `source_start` = `excluded`.`source_start`," +
		" `source_end` = `excluded`.`source_end`," +
		" `vector` = `excluded`.`vector`," +
		" `dimensions` = `excluded`.`dimensions`," +
		" `content_hash` = `excluded`.`content_hash`," +
		" `indexed_ts` = strftime('%s', 'now')" +
		" RETURNING `id`, `indexed_ts`"
	if err := d.db.QueryRowContext(ctx, stmt, args...).Scan(
		&upsert.ID,
		&upsert.IndexedTs,
	); err != nil {
		return nil, err
	}

	return upsert, nil
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

	query := "SELECT `id`, `generation_id`, `memo_id`, `memo_revision`, `chunk_ordinal`, `content_start`, `content_end`, `source_start`, `source_end`, `vector`, `dimensions`, `content_hash`, `indexed_ts` FROM `ai_index_chunk` WHERE " + strings.Join(where, " AND ") + " ORDER BY `id` ASC"
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
			&chunk.ContentHash,
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
