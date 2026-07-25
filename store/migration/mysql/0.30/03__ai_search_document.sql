-- ai_search_document stores provider-independent derived search documents for the
-- searchable memo corpus (NORMAL, top-level memos). Documents are derived data:
-- rebuildable from source and deletable without touching memos. memo_updated_ts and
-- content_hash record the source revision; projection_version and
-- normalization_version isolate rows built by other projection/normalization
-- versions; spans maps projected content byte ranges back to source content ranges.
CREATE TABLE `ai_search_document` (
  `id`                    INT          NOT NULL AUTO_INCREMENT PRIMARY KEY,
  `memo_id`               INT          NOT NULL,
  `memo_uid`              VARCHAR(255) NOT NULL,
  `memo_updated_ts`       BIGINT       NOT NULL,
  `content_hash`          VARCHAR(64)  NOT NULL,
  `projection_version`    INT          NOT NULL,
  `normalization_version` INT          NOT NULL,
  `title`                 TEXT         NOT NULL,
  `tags`                  JSON         NOT NULL,
  `content`               TEXT         NOT NULL,
  `spans`                 JSON         NOT NULL,
  `created_ts`            BIGINT       NOT NULL DEFAULT (UNIX_TIMESTAMP()),
  `updated_ts`            BIGINT       NOT NULL DEFAULT (UNIX_TIMESTAMP()),
  UNIQUE (`memo_id`)
);
