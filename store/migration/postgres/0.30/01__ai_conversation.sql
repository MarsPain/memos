-- ai_conversation stores private, owner-scoped AI chat conversations.
-- Conversations persist until the owner deletes them; chat history is never indexed as memos.
CREATE TABLE ai_conversation (
  id         SERIAL  PRIMARY KEY,
  uid        TEXT    NOT NULL UNIQUE,
  user_id    INTEGER NOT NULL,
  title      TEXT    NOT NULL DEFAULT '',
  created_ts BIGINT  NOT NULL DEFAULT EXTRACT(EPOCH FROM NOW()),
  updated_ts BIGINT  NOT NULL DEFAULT EXTRACT(EPOCH FROM NOW())
);

CREATE INDEX idx_ai_conversation_user_id ON ai_conversation(user_id);
