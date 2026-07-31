-- ai_index_generation stores one versioned embedding index generation per
-- fingerprint. The fingerprint identifies the embedding endpoint configuration
-- (provider, endpoint identity, model, dimensions) the generation was built
-- with, so index rows built by another configuration are isolated and can be
-- rebuilt. state is BUILDING, ACTIVE, or RETIRED; memo_total and memo_indexed
-- track build progress; last_error records the most recent build failure.
CREATE TABLE `ai_index_generation` (
  `id`                INT           NOT NULL AUTO_INCREMENT PRIMARY KEY,
  `fingerprint`       VARCHAR(255)  NOT NULL,
  `provider_id`       VARCHAR(255)  NOT NULL,
  `provider_type`     VARCHAR(64)   NOT NULL,
  `endpoint_identity` VARCHAR(255)  NOT NULL,
  `model`             VARCHAR(255)  NOT NULL,
  `dimensions`        INT           NOT NULL,
  `state`             VARCHAR(32)   NOT NULL,
  `memo_total`        INT           NOT NULL DEFAULT 0,
  `memo_indexed`      INT           NOT NULL DEFAULT 0,
  `last_error`        VARCHAR(1024) NOT NULL DEFAULT '',
  `created_ts`        BIGINT        NOT NULL DEFAULT (UNIX_TIMESTAMP()),
  `updated_ts`        BIGINT        NOT NULL DEFAULT (UNIX_TIMESTAMP()),
  UNIQUE (`fingerprint`)
);
