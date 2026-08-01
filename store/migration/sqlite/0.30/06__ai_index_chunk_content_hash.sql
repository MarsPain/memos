-- ai_index_chunk.content_hash records the content hash of the search document
-- the chunk was built from, so reconciliation compares source revisions and
-- hashes with chunk metadata: a document rewritten without a revision bump is
-- still detected as stale and rebuilt.
ALTER TABLE ai_index_chunk ADD COLUMN content_hash TEXT NOT NULL DEFAULT '';
