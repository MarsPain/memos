-- ai_index_generation.retired_ts records when the generation retired (0 while
-- it has not), so a retired generation is removed after a bounded grace
-- period following the atomic cutover.
ALTER TABLE ai_index_generation ADD COLUMN retired_ts BIGINT NOT NULL DEFAULT 0;
