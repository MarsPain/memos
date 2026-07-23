-- ai_conversation stores private, owner-scoped AI chat conversations.
-- Conversations persist until the owner deletes them; chat history is never indexed as memos.
CREATE TABLE `ai_conversation` (
  `id`         INT          NOT NULL AUTO_INCREMENT PRIMARY KEY,
  `uid`        VARCHAR(255) NOT NULL UNIQUE,
  `user_id`    INT          NOT NULL,
  `title`      TEXT         NOT NULL,
  `created_ts` BIGINT       NOT NULL DEFAULT (UNIX_TIMESTAMP()),
  `updated_ts` BIGINT       NOT NULL DEFAULT (UNIX_TIMESTAMP())
);

CREATE INDEX `idx_ai_conversation_user_id` ON `ai_conversation`(`user_id`);
