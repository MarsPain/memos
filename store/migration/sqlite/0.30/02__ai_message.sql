-- ai_message stores user messages and assistant attempts of AI chat conversations.
-- An assistant attempt links to its user message via parent_id and numbers from 1 via attempt;
-- a user message carries a client request ID unique within its conversation.
CREATE TABLE ai_message (
  id                INTEGER PRIMARY KEY AUTOINCREMENT,
  conversation_id   INTEGER NOT NULL,
  parent_id         INTEGER,
  attempt           INTEGER NOT NULL DEFAULT 0,
  role              TEXT    NOT NULL CHECK (role IN ('USER', 'ASSISTANT')),
  content           TEXT    NOT NULL DEFAULT '',
  status            TEXT    NOT NULL CHECK (status IN ('STREAMING', 'COMPLETE', 'FAILED', 'CANCELLED')),
  client_request_id TEXT,
  payload           TEXT    NOT NULL DEFAULT '{}',
  created_ts        BIGINT  NOT NULL DEFAULT (strftime('%s', 'now')),
  updated_ts        BIGINT  NOT NULL DEFAULT (strftime('%s', 'now')),
  UNIQUE (conversation_id, client_request_id),
  UNIQUE (parent_id, attempt),
  FOREIGN KEY (conversation_id) REFERENCES ai_conversation(id) ON DELETE CASCADE
);

CREATE INDEX idx_ai_message_conversation_id ON ai_message(conversation_id);
