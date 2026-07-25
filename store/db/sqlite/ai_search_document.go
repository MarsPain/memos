package sqlite

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/pkg/errors"

	"github.com/usememos/memos/store"
)

func (d *DB) UpsertAISearchDocument(ctx context.Context, upsert *store.AISearchDocument) (*store.AISearchDocument, error) {
	tagsBytes, err := marshalAISearchDocumentTags(upsert.Tags)
	if err != nil {
		return nil, err
	}
	spansBytes, err := marshalAISearchDocumentSpans(upsert.Spans)
	if err != nil {
		return nil, err
	}

	fields := []string{"`memo_id`", "`memo_uid`", "`memo_updated_ts`", "`content_hash`", "`projection_version`", "`normalization_version`", "`title`", "`tags`", "`content`", "`spans`"}
	placeholder := []string{"?", "?", "?", "?", "?", "?", "?", "?", "?", "?"}
	args := []any{upsert.MemoID, upsert.MemoUID, upsert.MemoUpdatedTs, upsert.ContentHash, upsert.ProjectionVersion, upsert.NormalizationVersion, upsert.Title, tagsBytes, upsert.Content, spansBytes}

	stmt := "INSERT INTO `ai_search_document` (" + strings.Join(fields, ", ") + ") VALUES (" + strings.Join(placeholder, ", ") + ")" +
		" ON CONFLICT(`memo_id`) DO UPDATE SET" +
		" `memo_uid` = `excluded`.`memo_uid`," +
		" `memo_updated_ts` = `excluded`.`memo_updated_ts`," +
		" `content_hash` = `excluded`.`content_hash`," +
		" `projection_version` = `excluded`.`projection_version`," +
		" `normalization_version` = `excluded`.`normalization_version`," +
		" `title` = `excluded`.`title`," +
		" `tags` = `excluded`.`tags`," +
		" `content` = `excluded`.`content`," +
		" `spans` = `excluded`.`spans`," +
		" `updated_ts` = strftime('%s', 'now')" +
		" RETURNING `id`, `created_ts`, `updated_ts`"
	if err := d.db.QueryRowContext(ctx, stmt, args...).Scan(
		&upsert.ID,
		&upsert.CreatedTs,
		&upsert.UpdatedTs,
	); err != nil {
		return nil, err
	}

	return upsert, nil
}

func (d *DB) ListAISearchDocuments(ctx context.Context, find *store.FindAISearchDocument) ([]*store.AISearchDocument, error) {
	where, args := []string{"1 = 1"}, []any{}

	if find.ID != nil {
		where, args = append(where, "`id` = ?"), append(args, *find.ID)
	}
	if find.MemoID != nil {
		where, args = append(where, "`memo_id` = ?"), append(args, *find.MemoID)
	}
	if find.IDGreaterThan != nil {
		where, args = append(where, "`id` > ?"), append(args, *find.IDGreaterThan)
	}

	query := "SELECT `id`, `memo_id`, `memo_uid`, `memo_updated_ts`, `content_hash`, `projection_version`, `normalization_version`, `title`, `tags`, `content`, `spans`, `created_ts`, `updated_ts` FROM `ai_search_document` WHERE " + strings.Join(where, " AND ") + " ORDER BY `id` ASC"
	if find.Limit != nil {
		query = fmt.Sprintf("%s LIMIT %d", query, *find.Limit)
	}
	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []*store.AISearchDocument{}
	for rows.Next() {
		document := &store.AISearchDocument{}
		var tagsBytes, spansBytes []byte
		if err := rows.Scan(
			&document.ID,
			&document.MemoID,
			&document.MemoUID,
			&document.MemoUpdatedTs,
			&document.ContentHash,
			&document.ProjectionVersion,
			&document.NormalizationVersion,
			&document.Title,
			&tagsBytes,
			&document.Content,
			&spansBytes,
			&document.CreatedTs,
			&document.UpdatedTs,
		); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(tagsBytes, &document.Tags); err != nil {
			return nil, errors.Wrap(err, "failed to unmarshal ai search document tags")
		}
		if err := json.Unmarshal(spansBytes, &document.Spans); err != nil {
			return nil, errors.Wrap(err, "failed to unmarshal ai search document spans")
		}
		list = append(list, document)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return list, nil
}

func (d *DB) DeleteAISearchDocument(ctx context.Context, delete *store.DeleteAISearchDocument) error {
	if _, err := d.db.ExecContext(ctx, "DELETE FROM `ai_search_document` WHERE `memo_id` = ?", delete.MemoID); err != nil {
		return errors.Wrap(err, "failed to delete ai search document")
	}
	return nil
}

func marshalAISearchDocumentTags(tags []string) (string, error) {
	if tags == nil {
		tags = []string{}
	}
	bytes, err := json.Marshal(tags)
	if err != nil {
		return "", errors.Wrap(err, "failed to marshal ai search document tags")
	}
	return string(bytes), nil
}

func marshalAISearchDocumentSpans(spans []*store.AISearchDocumentSpan) (string, error) {
	if spans == nil {
		spans = []*store.AISearchDocumentSpan{}
	}
	bytes, err := json.Marshal(spans)
	if err != nil {
		return "", errors.Wrap(err, "failed to marshal ai search document spans")
	}
	return string(bytes), nil
}
