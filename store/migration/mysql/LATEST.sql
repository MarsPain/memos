-- system_setting
CREATE TABLE `system_setting` (
  `name` VARCHAR(256) NOT NULL PRIMARY KEY,
  `value` LONGTEXT NOT NULL,
  `description` TEXT NOT NULL
);

-- user
CREATE TABLE `user` (
  `id` INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  `created_ts` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_ts` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `row_status` VARCHAR(256) NOT NULL DEFAULT 'NORMAL',
  `username` VARCHAR(256) NOT NULL UNIQUE,
  `role` VARCHAR(256) NOT NULL DEFAULT 'USER',
  `email` VARCHAR(256) NOT NULL DEFAULT '',
  `nickname` VARCHAR(256) NOT NULL DEFAULT '',
  `password_hash` VARCHAR(256) NOT NULL,
  `avatar_url` LONGTEXT NOT NULL,
  `description` VARCHAR(256) NOT NULL DEFAULT ''
);

-- user_setting
CREATE TABLE `user_setting` (
  `user_id` INT NOT NULL,
  `key` VARCHAR(256) NOT NULL,
  `value` LONGTEXT NOT NULL,
  UNIQUE(`user_id`,`key`)
);

-- memo
CREATE TABLE `memo` (
  `id` INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  `uid` VARCHAR(256) NOT NULL UNIQUE,
  `creator_id` INT NOT NULL,
  `created_ts` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_ts` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `row_status` VARCHAR(256) NOT NULL DEFAULT 'NORMAL',
  `content` TEXT NOT NULL,
  `visibility` VARCHAR(256) NOT NULL DEFAULT 'PRIVATE',
  `pinned` BOOLEAN NOT NULL DEFAULT FALSE,
  `payload` JSON NOT NULL
);

-- memo_relation
CREATE TABLE `memo_relation` (
  `memo_id` INT NOT NULL,
  `related_memo_id` INT NOT NULL,
  `type` VARCHAR(256) NOT NULL,
  UNIQUE(`memo_id`,`related_memo_id`,`type`)
);

-- attachment
CREATE TABLE `attachment` (
  `id` INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  `uid` VARCHAR(256) NOT NULL UNIQUE,
  `creator_id` INT NOT NULL,
  `created_ts` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_ts` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `filename` TEXT NOT NULL,
  `blob` MEDIUMBLOB,
  `type` VARCHAR(256) NOT NULL DEFAULT '',
  `size` INT NOT NULL DEFAULT '0',
  `memo_id` INT DEFAULT NULL,
  `storage_type` VARCHAR(256) NOT NULL DEFAULT '',
  `reference` TEXT NOT NULL DEFAULT (''),
  `payload` TEXT NOT NULL
);

-- idp
CREATE TABLE `idp` (
  `id` INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  `uid` VARCHAR(256) NOT NULL UNIQUE,
  `name` TEXT NOT NULL,
  `type` TEXT NOT NULL,
  `identifier_filter` VARCHAR(256) NOT NULL DEFAULT '',
  `config` TEXT NOT NULL
);

-- inbox
CREATE TABLE `inbox` (
  `id` INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  `created_ts` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `sender_id` INT NOT NULL,
  `receiver_id` INT NOT NULL,
  `status` TEXT NOT NULL,
  `message` TEXT NOT NULL
);

-- reaction
CREATE TABLE `reaction` (
  `id` INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  `created_ts` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `creator_id` INT NOT NULL,
  `content_id` VARCHAR(256) NOT NULL,
  `reaction_type` VARCHAR(256) NOT NULL,
  UNIQUE(`creator_id`,`content_id`,`reaction_type`)  
);

-- memo_share
CREATE TABLE `memo_share` (
  `id`         INT          NOT NULL AUTO_INCREMENT PRIMARY KEY,
  `uid`        VARCHAR(255) NOT NULL UNIQUE,
  `memo_id`    INT          NOT NULL,
  `creator_id` INT          NOT NULL,
  `created_ts` BIGINT       NOT NULL DEFAULT (UNIX_TIMESTAMP()),
  `expires_ts` BIGINT       DEFAULT NULL,
  FOREIGN KEY (`memo_id`) REFERENCES `memo`(`id`) ON DELETE CASCADE
);

CREATE INDEX `idx_memo_share_memo_id` ON `memo_share`(`memo_id`);

-- user_identity
CREATE TABLE `user_identity` (
  `id`         INT          NOT NULL AUTO_INCREMENT PRIMARY KEY,
  `user_id`    INT          NOT NULL,
  `provider`   VARCHAR(256) NOT NULL,
  `extern_uid` VARCHAR(256) NOT NULL,
  `created_ts` BIGINT       NOT NULL DEFAULT (UNIX_TIMESTAMP()),
  `updated_ts` BIGINT       NOT NULL DEFAULT (UNIX_TIMESTAMP()),
  UNIQUE (`provider`, `extern_uid`),
  UNIQUE (`user_id`, `provider`)
);

CREATE INDEX `idx_user_identity_user_id` ON `user_identity`(`user_id`);

-- ai_conversation
CREATE TABLE `ai_conversation` (
  `id`         INT          NOT NULL AUTO_INCREMENT PRIMARY KEY,
  `uid`        VARCHAR(255) NOT NULL UNIQUE,
  `user_id`    INT          NOT NULL,
  `title`      TEXT         NOT NULL,
  `created_ts` BIGINT       NOT NULL DEFAULT (UNIX_TIMESTAMP()),
  `updated_ts` BIGINT       NOT NULL DEFAULT (UNIX_TIMESTAMP())
);

CREATE INDEX `idx_ai_conversation_user_id` ON `ai_conversation`(`user_id`);

-- ai_message
CREATE TABLE `ai_message` (
  `id`                INT         NOT NULL AUTO_INCREMENT PRIMARY KEY,
  `conversation_id`   INT         NOT NULL,
  `parent_id`         INT,
  `attempt`           INT         NOT NULL DEFAULT 0,
  `role`              VARCHAR(32) NOT NULL,
  `content`           TEXT        NOT NULL,
  `status`            VARCHAR(32) NOT NULL,
  `client_request_id` VARCHAR(255),
  `payload`           JSON        NOT NULL,
  `created_ts`        BIGINT      NOT NULL DEFAULT (UNIX_TIMESTAMP()),
  `updated_ts`        BIGINT      NOT NULL DEFAULT (UNIX_TIMESTAMP()),
  UNIQUE (`conversation_id`, `client_request_id`),
  UNIQUE (`parent_id`, `attempt`),
  FOREIGN KEY (`conversation_id`) REFERENCES `ai_conversation`(`id`) ON DELETE CASCADE
);

CREATE INDEX `idx_ai_message_conversation_id` ON `ai_message`(`conversation_id`);

-- ai_search_document
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

-- ai_index_generation
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
  `retired_ts`        BIGINT        NOT NULL DEFAULT 0,
  UNIQUE (`fingerprint`)
);

-- ai_index_chunk
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
  `content_hash`  VARCHAR(64) NOT NULL DEFAULT '',
  `indexed_ts`    BIGINT     NOT NULL DEFAULT (UNIX_TIMESTAMP()),
  UNIQUE (`generation_id`, `memo_id`, `memo_revision`, `chunk_ordinal`)
);
CREATE INDEX `idx_ai_index_chunk_generation_id_id` ON `ai_index_chunk`(`generation_id`, `id`);
CREATE INDEX `idx_ai_index_chunk_memo_id` ON `ai_index_chunk`(`memo_id`);
