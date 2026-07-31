-- ai_index_generation stores one versioned embedding index generation per
-- fingerprint. The fingerprint identifies the embedding endpoint configuration
-- (provider, endpoint identity, model, dimensions) the generation was built
-- with, so index rows built by another configuration are isolated and can be
-- rebuilt. state is BUILDING, ACTIVE, or RETIRED; memo_total and memo_indexed
-- track build progress; last_error records the most recent build failure.
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
