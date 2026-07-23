-- ai_message stores user messages and assistant attempts of AI chat conversations.
-- An assistant attempt links to its user message via parent_id and numbers from 1 via attempt;
-- a user message carries a client request ID unique within its conversation.
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
