package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/pkg/errors"

	"github.com/usememos/memos/store"
)

func (d *DB) CreateAIConversation(ctx context.Context, create *store.AIConversation) (*store.AIConversation, error) {
	fields := []string{"uid", "user_id", "title"}
	args := []any{create.UID, create.UserID, create.Title}

	stmt := "INSERT INTO ai_conversation (" + strings.Join(fields, ", ") + ") VALUES (" + placeholders(len(args)) + ") RETURNING id, created_ts, updated_ts"
	if err := d.db.QueryRowContext(ctx, stmt, args...).Scan(
		&create.ID,
		&create.CreatedTs,
		&create.UpdatedTs,
	); err != nil {
		return nil, err
	}

	return create, nil
}

func (d *DB) ListAIConversations(ctx context.Context, find *store.FindAIConversation) ([]*store.AIConversation, error) {
	where, args := []string{"1 = 1"}, []any{}

	if find.ID != nil {
		where, args = append(where, "id = "+placeholder(len(args)+1)), append(args, *find.ID)
	}
	if find.UID != nil {
		where, args = append(where, "uid = "+placeholder(len(args)+1)), append(args, *find.UID)
	}
	if find.UserID != nil {
		where, args = append(where, "user_id = "+placeholder(len(args)+1)), append(args, *find.UserID)
	}

	query := "SELECT id, uid, user_id, title, created_ts, updated_ts FROM ai_conversation WHERE " + strings.Join(where, " AND ") + " ORDER BY updated_ts DESC, id DESC"
	if find.Limit != nil {
		query = fmt.Sprintf("%s LIMIT %d", query, *find.Limit)
		if find.Offset != nil {
			query = fmt.Sprintf("%s OFFSET %d", query, *find.Offset)
		}
	}
	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []*store.AIConversation{}
	for rows.Next() {
		conversation := &store.AIConversation{}
		if err := rows.Scan(
			&conversation.ID,
			&conversation.UID,
			&conversation.UserID,
			&conversation.Title,
			&conversation.CreatedTs,
			&conversation.UpdatedTs,
		); err != nil {
			return nil, err
		}
		list = append(list, conversation)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return list, nil
}

func (d *DB) UpdateAIConversation(ctx context.Context, update *store.UpdateAIConversation) error {
	set, args := []string{}, []any{}

	if v := update.Title; v != nil {
		set, args = append(set, "title = "+placeholder(len(args)+1)), append(args, *v)
	}
	if v := update.UpdatedTs; v != nil {
		set, args = append(set, "updated_ts = "+placeholder(len(args)+1)), append(args, *v)
	}

	args = append(args, update.ID)
	stmt := "UPDATE ai_conversation SET " + strings.Join(set, ", ") + " WHERE id = " + placeholder(len(args))
	result, err := d.db.ExecContext(ctx, stmt, args...)
	if err != nil {
		return errors.Wrap(err, "failed to update ai conversation")
	}
	if _, err := result.RowsAffected(); err != nil {
		return err
	}
	return nil
}

func (d *DB) DeleteAIConversation(ctx context.Context, delete *store.DeleteAIConversation) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return errors.Wrap(err, "failed to start ai conversation delete transaction")
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	// Messages are deleted explicitly instead of relying on the FK cascade,
	// matching the sqlite driver which runs with foreign keys disabled.
	if _, err := tx.ExecContext(ctx, "DELETE FROM ai_message WHERE conversation_id = $1", delete.ID); err != nil {
		return errors.Wrap(err, "failed to delete ai messages")
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM ai_conversation WHERE id = $1", delete.ID); err != nil {
		return errors.Wrap(err, "failed to delete ai conversation")
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	tx = nil
	return nil
}
