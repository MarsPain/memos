package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/pkg/errors"
	"google.golang.org/protobuf/encoding/protojson"

	storepb "github.com/usememos/memos/proto/gen/store"
	"github.com/usememos/memos/store"
)

func (d *DB) CreateAIMessageWithAttempt(ctx context.Context, message *store.AIMessage, attempt *store.AIMessage) (*store.AIMessage, *store.AIMessage, error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to start ai message create transaction")
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	message, err = insertAIMessage(ctx, tx, message)
	if err != nil {
		return nil, nil, err
	}
	attempt.ParentID = &message.ID
	attempt.Attempt = 1
	attempt, err = insertAIMessage(ctx, tx, attempt)
	if err != nil {
		return nil, nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	tx = nil
	return message, attempt, nil
}

func (d *DB) CreateAIMessageAttempt(ctx context.Context, attempt *store.AIMessage) (*store.AIMessage, error) {
	// The next attempt number is a read-modify-write, so run serializable.
	tx, err := d.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, errors.Wrap(err, "failed to start ai message attempt transaction")
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	var nextAttempt int32
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(`attempt`), 0) + 1 FROM `ai_message` WHERE `parent_id` = ?", attempt.ParentID).Scan(&nextAttempt); err != nil {
		return nil, errors.Wrap(err, "failed to compute next ai message attempt")
	}
	attempt.Attempt = nextAttempt

	attempt, err = insertAIMessage(ctx, tx, attempt)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return attempt, nil
}

func (d *DB) ListAIMessages(ctx context.Context, find *store.FindAIMessage) ([]*store.AIMessage, error) {
	where, args := []string{"1 = 1"}, []any{}

	if find.ID != nil {
		where, args = append(where, "`id` = ?"), append(args, *find.ID)
	}
	if find.ConversationID != nil {
		where, args = append(where, "`conversation_id` = ?"), append(args, *find.ConversationID)
	}
	if find.ParentID != nil {
		where, args = append(where, "`parent_id` = ?"), append(args, *find.ParentID)
	}
	if find.ClientRequestID != nil {
		where, args = append(where, "`client_request_id` = ?"), append(args, *find.ClientRequestID)
	}
	if find.Status != nil {
		where, args = append(where, "`status` = ?"), append(args, find.Status.String())
	}

	query := "SELECT `id`, `conversation_id`, `parent_id`, `attempt`, `role`, `content`, `status`, `client_request_id`, `payload`, `created_ts`, `updated_ts` FROM `ai_message` WHERE " + strings.Join(where, " AND ") + " ORDER BY `id` ASC"
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

	list := []*store.AIMessage{}
	for rows.Next() {
		message := &store.AIMessage{}
		var payloadBytes []byte
		if err := rows.Scan(
			&message.ID,
			&message.ConversationID,
			&message.ParentID,
			&message.Attempt,
			&message.Role,
			&message.Content,
			&message.Status,
			&message.ClientRequestID,
			&payloadBytes,
			&message.CreatedTs,
			&message.UpdatedTs,
		); err != nil {
			return nil, err
		}

		payload := &storepb.AIMessagePayload{}
		if err := protojsonUnmarshaler.Unmarshal(payloadBytes, payload); err != nil {
			return nil, err
		}
		message.Payload = payload
		list = append(list, message)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return list, nil
}

func (d *DB) UpdateAIMessage(ctx context.Context, update *store.UpdateAIMessage) error {
	set, args := []string{}, []any{}

	if v := update.Content; v != nil {
		set, args = append(set, "`content` = ?"), append(args, *v)
	}
	if v := update.Status; v != nil {
		set, args = append(set, "`status` = ?"), append(args, v.String())
	}
	if v := update.Payload; v != nil {
		bytes, err := protojson.Marshal(v)
		if err != nil {
			return errors.Wrap(err, "failed to marshal ai message payload")
		}
		set, args = append(set, "`payload` = ?"), append(args, string(bytes))
	}
	if v := update.UpdatedTs; v != nil {
		set, args = append(set, "`updated_ts` = ?"), append(args, *v)
	}

	args = append(args, update.ID)
	stmt := "UPDATE `ai_message` SET " + strings.Join(set, ", ") + " WHERE `id` = ?"
	result, err := d.db.ExecContext(ctx, stmt, args...)
	if err != nil {
		return errors.Wrap(err, "failed to update ai message")
	}
	if _, err := result.RowsAffected(); err != nil {
		return err
	}
	return nil
}

// insertAIMessage inserts a message inside tx, returning it with the
// database-assigned ID and timestamps filled in.
func insertAIMessage(ctx context.Context, tx *sql.Tx, message *store.AIMessage) (*store.AIMessage, error) {
	payloadString := "{}"
	if message.Payload != nil {
		bytes, err := protojson.Marshal(message.Payload)
		if err != nil {
			return nil, errors.Wrap(err, "failed to marshal ai message payload")
		}
		payloadString = string(bytes)
	}

	fields := []string{"`conversation_id`", "`parent_id`", "`attempt`", "`role`", "`content`", "`status`", "`client_request_id`", "`payload`"}
	placeholder := []string{"?", "?", "?", "?", "?", "?", "?", "?"}
	args := []any{message.ConversationID, message.ParentID, message.Attempt, message.Role.String(), message.Content, message.Status.String(), message.ClientRequestID, payloadString}

	stmt := "INSERT INTO `ai_message` (" + strings.Join(fields, ", ") + ") VALUES (" + strings.Join(placeholder, ", ") + ")"
	result, err := tx.ExecContext(ctx, stmt, args...)
	if err != nil {
		return nil, err
	}

	rawID, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	message.ID = int32(rawID)
	if err := tx.QueryRowContext(ctx, "SELECT `created_ts`, `updated_ts` FROM `ai_message` WHERE `id` = ?", message.ID).Scan(
		&message.CreatedTs,
		&message.UpdatedTs,
	); err != nil {
		return nil, err
	}

	return message, nil
}
