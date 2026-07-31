-- system_setting
CREATE TABLE system_setting (
  name TEXT NOT NULL,
  value TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  UNIQUE(name)
);

-- user
CREATE TABLE user (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  created_ts BIGINT NOT NULL DEFAULT (strftime('%s', 'now')),
  updated_ts BIGINT NOT NULL DEFAULT (strftime('%s', 'now')),
  row_status TEXT NOT NULL CHECK (row_status IN ('NORMAL', 'ARCHIVED')) DEFAULT 'NORMAL',
  username TEXT NOT NULL UNIQUE,
  role TEXT NOT NULL DEFAULT 'USER',
  email TEXT NOT NULL DEFAULT '',
  nickname TEXT NOT NULL DEFAULT '',
  password_hash TEXT NOT NULL,
  avatar_url TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT ''
);

-- user_setting
CREATE TABLE user_setting (
  user_id INTEGER NOT NULL,
  key TEXT NOT NULL,
  value TEXT NOT NULL,
  UNIQUE(user_id, key)
);

-- memo
CREATE TABLE memo (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  uid TEXT NOT NULL UNIQUE,
  creator_id INTEGER NOT NULL,
  created_ts BIGINT NOT NULL DEFAULT (strftime('%s', 'now')),
  updated_ts BIGINT NOT NULL DEFAULT (strftime('%s', 'now')),
  row_status TEXT NOT NULL CHECK (row_status IN ('NORMAL', 'ARCHIVED')) DEFAULT 'NORMAL',
  content TEXT NOT NULL DEFAULT '',
  visibility TEXT NOT NULL CHECK (visibility IN ('PUBLIC', 'PROTECTED', 'PRIVATE')) DEFAULT 'PRIVATE',
  pinned INTEGER NOT NULL CHECK (pinned IN (0, 1)) DEFAULT 0,
  payload TEXT NOT NULL DEFAULT '{}'
);

-- memo_relation
CREATE TABLE memo_relation (
  memo_id INTEGER NOT NULL,
  related_memo_id INTEGER NOT NULL,
  type TEXT NOT NULL,
  UNIQUE(memo_id, related_memo_id, type)
);

-- attachment
CREATE TABLE attachment (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  uid TEXT NOT NULL UNIQUE,
  creator_id INTEGER NOT NULL,
  created_ts BIGINT NOT NULL DEFAULT (strftime('%s', 'now')),
  updated_ts BIGINT NOT NULL DEFAULT (strftime('%s', 'now')),
  filename TEXT NOT NULL DEFAULT '',
  blob BLOB DEFAULT NULL,
  type TEXT NOT NULL DEFAULT '',
  size INTEGER NOT NULL DEFAULT 0,
  memo_id INTEGER,
  storage_type TEXT NOT NULL DEFAULT '',
  reference TEXT NOT NULL DEFAULT '',
  payload TEXT NOT NULL DEFAULT '{}'
);

-- idp
CREATE TABLE idp (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  uid TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  type TEXT NOT NULL,
  identifier_filter TEXT NOT NULL DEFAULT '',
  config TEXT NOT NULL DEFAULT '{}'
);

-- inbox
CREATE TABLE inbox (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  created_ts BIGINT NOT NULL DEFAULT (strftime('%s', 'now')),
  sender_id INTEGER NOT NULL,
  receiver_id INTEGER NOT NULL,
  status TEXT NOT NULL,
  message TEXT NOT NULL DEFAULT '{}'
);

-- reaction
CREATE TABLE reaction (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  created_ts BIGINT NOT NULL DEFAULT (strftime('%s', 'now')),
  creator_id INTEGER NOT NULL,
  content_id TEXT NOT NULL,
  reaction_type TEXT NOT NULL,
  UNIQUE(creator_id, content_id, reaction_type)
);

-- memo_share
CREATE TABLE memo_share (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  uid        TEXT    NOT NULL UNIQUE,
  memo_id    INTEGER NOT NULL,
  creator_id INTEGER NOT NULL,
  created_ts BIGINT  NOT NULL DEFAULT (strftime('%s', 'now')),
  expires_ts BIGINT  DEFAULT NULL,
  FOREIGN KEY (memo_id) REFERENCES memo(id) ON DELETE CASCADE
);

CREATE INDEX idx_memo_share_memo_id ON memo_share(memo_id);

-- user_identity
CREATE TABLE user_identity (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id    INTEGER NOT NULL,
  provider   TEXT    NOT NULL,
  extern_uid TEXT    NOT NULL,
  created_ts BIGINT  NOT NULL DEFAULT (strftime('%s', 'now')),
  updated_ts BIGINT  NOT NULL DEFAULT (strftime('%s', 'now')),
  UNIQUE (provider, extern_uid),
  UNIQUE (user_id, provider)
);

CREATE INDEX idx_user_identity_user_id ON user_identity(user_id);

-- ai_conversation
CREATE TABLE ai_conversation (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  uid        TEXT    NOT NULL UNIQUE,
  user_id    INTEGER NOT NULL,
  title      TEXT    NOT NULL DEFAULT '',
  created_ts BIGINT  NOT NULL DEFAULT (strftime('%s', 'now')),
  updated_ts BIGINT  NOT NULL DEFAULT (strftime('%s', 'now'))
);

CREATE INDEX idx_ai_conversation_user_id ON ai_conversation(user_id);

-- ai_message
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

-- ai_search_document
CREATE TABLE ai_search_document (
  id                    INTEGER PRIMARY KEY AUTOINCREMENT,
  memo_id               INTEGER NOT NULL UNIQUE,
  memo_uid              TEXT    NOT NULL,
  memo_updated_ts       BIGINT  NOT NULL,
  content_hash          TEXT    NOT NULL,
  projection_version    INTEGER NOT NULL,
  normalization_version INTEGER NOT NULL,
  title                 TEXT    NOT NULL DEFAULT '',
  tags                  TEXT    NOT NULL DEFAULT '[]',
  content               TEXT    NOT NULL DEFAULT '',
  spans                 TEXT    NOT NULL DEFAULT '[]',
  created_ts            BIGINT  NOT NULL DEFAULT (strftime('%s', 'now')),
  updated_ts            BIGINT  NOT NULL DEFAULT (strftime('%s', 'now'))
);

-- ai_index_generation
CREATE TABLE ai_index_generation (
  id                INTEGER PRIMARY KEY AUTOINCREMENT,
  fingerprint       TEXT    NOT NULL UNIQUE,
  provider_id       TEXT    NOT NULL,
  provider_type     TEXT    NOT NULL,
  endpoint_identity TEXT    NOT NULL,
  model             TEXT    NOT NULL,
  dimensions        INTEGER NOT NULL,
  state             TEXT    NOT NULL,
  memo_total        INTEGER NOT NULL DEFAULT 0,
  memo_indexed      INTEGER NOT NULL DEFAULT 0,
  last_error        TEXT    NOT NULL DEFAULT '',
  created_ts        BIGINT  NOT NULL DEFAULT (strftime('%s', 'now')),
  updated_ts        BIGINT  NOT NULL DEFAULT (strftime('%s', 'now'))
);

-- ai_index_chunk
CREATE TABLE ai_index_chunk (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  generation_id INTEGER NOT NULL,
  memo_id       INTEGER NOT NULL,
  memo_revision BIGINT  NOT NULL,
  chunk_ordinal INTEGER NOT NULL,
  content_start INTEGER NOT NULL,
  content_end   INTEGER NOT NULL,
  source_start  INTEGER NOT NULL,
  source_end    INTEGER NOT NULL,
  vector        BLOB    NOT NULL,
  dimensions    INTEGER NOT NULL,
  indexed_ts    BIGINT  NOT NULL DEFAULT (strftime('%s', 'now')),
  UNIQUE (generation_id, memo_id, memo_revision, chunk_ordinal)
);
CREATE INDEX idx_ai_index_chunk_generation_id_id ON ai_index_chunk(generation_id, id);
CREATE INDEX idx_ai_index_chunk_memo_id ON ai_index_chunk(memo_id);
