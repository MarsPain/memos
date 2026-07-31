-- ai_index_chunk stores one embedded chunk of a memo search document within an
-- index generation. vector is the little-endian float32 embedding of the chunk
-- (internal/ai vector encoding version 1); memo_revision records the search
-- document revision the chunk was built from. The unique key
-- (generation_id, memo_id, memo_revision, chunk_ordinal) makes chunk upserts
-- idempotent; the (generation_id, id) index supports keyset scans.
CREATE TABLE `ai_index_chunk` (
  `id`            INT        NOT NULL AUTO_INCREMENT PRIMARY KEY,
  `generation_id` INT        NOT NULL,
  `memo_id`       INT        NOT NULL,
  `memo_revision` BIGINT     NOT NULL,
  `chunk_ordinal` INT        NOT NULL,
  `content_start` INT        NOT NULL,
  `content_end`   INT        NOT NULL,
  `source_start`  INT        NOT NULL,
  `source_end`    INT        NOT NULL,
  `vector`        MEDIUMBLOB NOT NULL,
  `dimensions`    INT        NOT NULL,
  `indexed_ts`    BIGINT     NOT NULL DEFAULT (UNIX_TIMESTAMP()),
  UNIQUE (`generation_id`, `memo_id`, `memo_revision`, `chunk_ordinal`)
);

CREATE INDEX `idx_ai_index_chunk_generation_id_id` ON `ai_index_chunk`(`generation_id`, `id`);
CREATE INDEX `idx_ai_index_chunk_memo_id` ON `ai_index_chunk`(`memo_id`);
