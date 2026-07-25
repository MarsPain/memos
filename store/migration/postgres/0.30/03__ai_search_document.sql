-- ai_search_document stores provider-independent derived search documents for the
-- searchable memo corpus (NORMAL, top-level memos). Documents are derived data:
-- rebuildable from source and deletable without touching memos. memo_updated_ts and
-- content_hash record the source revision; projection_version and
-- normalization_version isolate rows built by other projection/normalization
-- versions; spans maps projected content byte ranges back to source content ranges.
CREATE TABLE ai_search_document (
  id                    SERIAL  PRIMARY KEY,
  memo_id               INTEGER NOT NULL,
  memo_uid              TEXT    NOT NULL,
  memo_updated_ts       BIGINT  NOT NULL,
  content_hash          TEXT    NOT NULL,
  projection_version    INTEGER NOT NULL,
  normalization_version INTEGER NOT NULL,
  title                 TEXT    NOT NULL DEFAULT '',
  tags                  JSONB   NOT NULL DEFAULT '[]',
  content               TEXT    NOT NULL DEFAULT '',
  spans                 JSONB   NOT NULL DEFAULT '[]',
  created_ts            BIGINT  NOT NULL DEFAULT EXTRACT(EPOCH FROM NOW()),
  updated_ts            BIGINT  NOT NULL DEFAULT EXTRACT(EPOCH FROM NOW()),
  UNIQUE (memo_id)
);
