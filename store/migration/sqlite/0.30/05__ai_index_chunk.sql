-- ai_index_chunk stores one embedded chunk of a memo search document within an
-- index generation. vector is the little-endian float32 embedding of the chunk
-- (internal/ai vector encoding version 1); memo_revision records the search
-- document revision the chunk was built from. The unique key
-- (generation_id, memo_id, memo_revision, chunk_ordinal) makes chunk upserts
-- idempotent; the (generation_id, id) index supports keyset scans.
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
