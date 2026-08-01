package search

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/pkg/errors"

	internalai "github.com/usememos/memos/internal/ai"
	"github.com/usememos/memos/internal/ai/gateway"
	"github.com/usememos/memos/store"
)

// Generation lifecycle states, shared with the store schema. At most one
// generation is ACTIVE; a BUILDING generation never serves queries; a RETIRED
// generation is removed after a bounded grace period.
const (
	// generationStateBuilding marks a generation whose indexing pass has not
	// completed. A building generation never serves queries.
	generationStateBuilding = store.AIIndexGenerationBuilding
	// generationStateActive marks the generation semantic queries scan.
	generationStateActive = store.AIIndexGenerationActive
	// generationStateRetired marks a generation replaced by a newer active
	// one, kept for a bounded grace period before removal.
	generationStateRetired = store.AIIndexGenerationRetired
)

const (
	// indexScanBatchSize bounds every search-document read of a
	// reconciliation sweep.
	indexScanBatchSize = 100
	// indexEmbedBatchSize bounds every embedding call of a pass.
	indexEmbedBatchSize = 16
	// retiredGenerationGracePeriod bounds how long a retired generation is
	// kept before removal: in-flight scans finish on the new active
	// generation, and a configuration revert inside the window can resume
	// the retired generation instead of rebuilding it.
	retiredGenerationGracePeriod = 10 * time.Minute
)

// Indexer keeps the embedding index generations in step with the search
// documents. The runner drives it after the Stage 2 search-document refresh:
// a targeted sync follows invalidation signals, and a periodic full
// reconciliation sweep repairs dropped signals. Progress derives from the
// stored chunks rather than a durable queue, so a restart mid-pass resumes
// without redoing committed work. Per-memo backoff keeps one poison memo from
// starving the corpus. Projection, chunking, and embedding stay outside the
// memo write path; normal writes never wait for embedding.
type Indexer struct {
	store        *store.Store
	modelFactory func() gateway.ModelFactory

	// mu serializes passes so only one local worker indexes at a time; it
	// also guards backoff.
	mu      sync.Mutex
	backoff *backoffTracker
}

// NewIndexer creates the embedding indexer. The model factory indirection
// lets tests swap the provider adapters after construction.
func NewIndexer(s *store.Store, modelFactory func() gateway.ModelFactory) *Indexer {
	return &Indexer{store: s, modelFactory: modelFactory, backoff: newBackoffTracker()}
}

// factory returns the configured model factory, or nil for the production
// adapters.
func (ix *Indexer) factory() gateway.ModelFactory {
	if ix.modelFactory == nil {
		return nil
	}
	return ix.modelFactory()
}

// RunOnce runs one full reconciliation sweep over the search documents:
// every document whose chunks are missing or built from another memo
// revision is re-chunked and re-embedded in bounded batches. The sweep uses
// stable keyset pagination and honors cancellation between batches; documents
// whose embedding keeps failing back off individually while the rest of the
// corpus keeps progressing.
//
// The sweep drives the generation lifecycle. Expired retired generations are
// removed first, with or without an embedding assignment. With embeddings
// disabled no building work runs: builders stay inert, never served and
// never embedded. A newer desired fingerprint supersedes and removes older
// building generations. When the sweep confirms every eligible document has
// current chunks, the building generation promotes atomically — but only
// while it still matches the desired embedding assignment. With no embedding
// assignment it does no indexing work.
func (ix *Indexer) RunOnce(ctx context.Context) error {
	ix.mu.Lock()
	defer ix.mu.Unlock()

	// Housekeeping first: retired generations past the grace period are
	// removed with their chunks, independent of the embedding configuration.
	ix.removeExpiredRetiredGenerations(ctx)

	setting, err := ix.store.GetInstanceAISetting(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to get AI setting")
	}
	assignment := resolveEmbeddingAssignment(setting)
	if assignment == nil {
		// Embeddings disabled: building work is cancelled. Building
		// generations stay inert — never served, never embedded — until
		// embeddings are configured again, when a matching fingerprint resumes
		// them and a newer one supersedes and removes them.
		return nil
	}
	desired := assignment.indexFingerprint()

	// A newer desired fingerprint supersedes and removes any older building
	// generation; the retired and active ones keep their lifecycle.
	if err := ix.removeSupersededBuilders(ctx, desired); err != nil {
		return err
	}

	generation, err := ix.desiredGeneration(ctx, assignment)
	if err != nil {
		return err
	}

	model, err := assignment.resolveModel(ix.factory())
	if err != nil {
		return errors.Wrap(err, "failed to resolve embedding model")
	}

	total, indexed := 0, 0
	var afterID int32
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		limit := indexScanBatchSize
		documents, err := ix.store.ListAISearchDocuments(ctx, &store.FindAISearchDocument{IDGreaterThan: &afterID, Limit: &limit})
		if err != nil {
			return errors.Wrap(err, "failed to list search documents")
		}
		for _, document := range documents {
			afterID = document.ID
			if document.ProjectionVersion != ProjectionVersion || document.NormalizationVersion != NormalizationVersion {
				continue
			}
			total++
			if ix.syncDocument(ctx, generation, assignment, model, document) {
				indexed++
			}
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		if len(documents) < limit {
			break
		}
	}

	generation.MemoTotal = int32(total)
	generation.MemoIndexed = int32(indexed)
	complete := indexed == total
	if complete {
		// The verification pass confirmed every eligible document observed in
		// the pass has current chunks: earlier build failures are resolved.
		generation.LastError = ""
	}
	if generation.State != generationStateBuilding || !complete {
		// An active generation stays active through partial sweeps — it keeps
		// serving while the stale documents catch up; a building generation
		// with rejected batches keeps building: a partial index never serves
		// queries.
		if _, err := ix.store.UpsertAIIndexGeneration(ctx, generation); err != nil {
			return errors.Wrap(err, "failed to update index generation")
		}
		return nil
	}
	return ix.promote(ctx, generation, desired)
}

// promote atomically cuts a verified building generation over to active. The
// promotion is conditional on the generation still matching the desired
// embedding assignment, checked against the current setting rather than the
// one the sweep started with: a desired fingerprint that moved on mid-build
// supersedes and removes this stale builder, and embeddings disabled
// mid-build leave it inert.
func (ix *Indexer) promote(ctx context.Context, generation *store.AIIndexGeneration, desired string) error {
	current, err := ix.desiredFingerprint(ctx)
	if err != nil {
		return err
	}
	if current != desired {
		if current == "" {
			// Embeddings were disabled mid-build: the builder stays inert,
			// but the completed pass's progress is still recorded.
			if _, err := ix.store.UpsertAIIndexGeneration(ctx, generation); err != nil {
				return errors.Wrap(err, "failed to update index generation")
			}
			return nil
		}
		// A newer desired fingerprint supersedes and removes this stale
		// builder; its replacement is built by a later pass.
		return ix.removeGeneration(ctx, generation.ID)
	}
	if _, err := ix.store.UpsertAIIndexGeneration(ctx, generation); err != nil {
		return errors.Wrap(err, "failed to update index generation")
	}
	promoted, err := ix.store.PromoteAIIndexGeneration(ctx, &store.AIIndexGenerationPromotion{
		ID:          generation.ID,
		Fingerprint: desired,
		RetiredTs:   time.Now().Unix(),
	})
	if err != nil {
		return errors.Wrap(err, "failed to promote index generation")
	}
	if promoted {
		slog.Info("embedding index generation promoted",
			slog.Int64("generation_id", int64(generation.ID)),
			slog.Int("memo_indexed", int(generation.MemoIndexed)))
	}
	return nil
}

// desiredFingerprint returns the desired generation fingerprint under the
// current instance setting, or "" when no embedding capability is assigned.
func (ix *Indexer) desiredFingerprint(ctx context.Context) (string, error) {
	setting, err := ix.store.GetInstanceAISetting(ctx)
	if err != nil {
		return "", errors.Wrap(err, "failed to get AI setting")
	}
	assignment := resolveEmbeddingAssignment(setting)
	if assignment == nil {
		return "", nil
	}
	return assignment.indexFingerprint(), nil
}

// removeSupersededBuilders removes every building generation whose
// fingerprint is not the desired one, chunks included: a superseded builder
// can never promote, so keeping it would only leak derived rows.
func (ix *Indexer) removeSupersededBuilders(ctx context.Context, desired string) error {
	buildingState := generationStateBuilding
	builders, err := ix.store.ListAIIndexGenerations(ctx, &store.FindAIIndexGeneration{State: &buildingState})
	if err != nil {
		return errors.Wrap(err, "failed to list building generations")
	}
	for _, builder := range builders {
		if builder.Fingerprint == desired {
			continue
		}
		if err := ix.removeGeneration(ctx, builder.ID); err != nil {
			return err
		}
	}
	return nil
}

// removeExpiredRetiredGenerations removes retired generations whose grace
// period elapsed, chunks included. Cleanup failures are logged and left to
// the next pass rather than failing the sweep.
func (ix *Indexer) removeExpiredRetiredGenerations(ctx context.Context) {
	retiredState := generationStateRetired
	retired, err := ix.store.ListAIIndexGenerations(ctx, &store.FindAIIndexGeneration{State: &retiredState})
	if err != nil {
		slog.Warn("failed to list retired generations", slog.Any("err", err))
		return
	}
	cutoff := time.Now().Add(-retiredGenerationGracePeriod).Unix()
	for _, generation := range retired {
		if generation.RetiredTs == 0 || generation.RetiredTs > cutoff {
			continue
		}
		if err := ix.removeGeneration(ctx, generation.ID); err != nil {
			slog.Warn("failed to remove expired retired generation", slog.Int64("generation_id", int64(generation.ID)), slog.Any("err", err))
		}
	}
}

// removeGeneration deletes a generation and every chunk derived into it.
func (ix *Indexer) removeGeneration(ctx context.Context, generationID int32) error {
	if err := ix.store.DeleteAIIndexChunk(ctx, &store.DeleteAIIndexChunk{GenerationID: &generationID}); err != nil {
		return errors.Wrap(err, "failed to delete generation chunks")
	}
	if err := ix.store.DeleteAIIndexGeneration(ctx, &store.DeleteAIIndexGeneration{ID: &generationID}); err != nil {
		return errors.Wrap(err, "failed to delete index generation")
	}
	return nil
}

// SyncMemos reconciles the index for the named memos after their search
// documents changed. A memo without a search document left the corpus
// (deleted or archived): its derived chunks are removed from every
// generation. Chunk removal runs with or without an embedding assignment;
// embedding only runs when one is configured.
func (ix *Indexer) SyncMemos(ctx context.Context, memoIDs []int32) error {
	ix.mu.Lock()
	defer ix.mu.Unlock()

	var pending []*store.AISearchDocument
	for _, memoID := range memoIDs {
		if err := ctx.Err(); err != nil {
			return err
		}
		document, err := ix.store.GetAISearchDocument(ctx, &store.FindAISearchDocument{MemoID: &memoID})
		if err != nil {
			return errors.Wrap(err, "failed to get search document")
		}
		if document == nil {
			// The memo left the corpus: its derived chunks go from every
			// generation.
			if err := ix.store.DeleteAIIndexChunk(ctx, &store.DeleteAIIndexChunk{MemoID: &memoID}); err != nil {
				return errors.Wrap(err, "failed to delete index chunks")
			}
			ix.recordResult(memoID, nil)
			continue
		}
		pending = append(pending, document)
	}
	if len(pending) == 0 {
		return nil
	}

	setting, err := ix.store.GetInstanceAISetting(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to get AI setting")
	}
	assignment := resolveEmbeddingAssignment(setting)
	if assignment == nil {
		return nil
	}
	generation, err := ix.desiredGeneration(ctx, assignment)
	if err != nil {
		return err
	}
	model, err := assignment.resolveModel(ix.factory())
	if err != nil {
		return errors.Wrap(err, "failed to resolve embedding model")
	}

	for _, document := range pending {
		if err := ctx.Err(); err != nil {
			return err
		}
		if document.ProjectionVersion != ProjectionVersion || document.NormalizationVersion != NormalizationVersion {
			continue
		}
		ix.syncDocument(ctx, generation, assignment, model, document)
	}
	return nil
}

// desiredGeneration returns the generation matching the desired embedding
// fingerprint, creating it in the building state when it does not exist yet.
// A retired generation whose fingerprint is desired again — the configuration
// reverted inside the grace period — resumes building with its still-stored
// chunks as a head start and must pass the verification pass before serving
// again.
func (ix *Indexer) desiredGeneration(ctx context.Context, assignment *embeddingAssignment) (*store.AIIndexGeneration, error) {
	fingerprint := assignment.indexFingerprint()
	generation, err := ix.store.GetAIIndexGeneration(ctx, &store.FindAIIndexGeneration{Fingerprint: &fingerprint})
	if err != nil {
		return nil, errors.Wrap(err, "failed to find index generation")
	}
	if generation == nil {
		generation, err = ix.store.UpsertAIIndexGeneration(ctx, &store.AIIndexGeneration{
			Fingerprint:      fingerprint,
			ProviderID:       assignment.provider.ID,
			ProviderType:     string(assignment.provider.Type),
			EndpointIdentity: endpointIdentity(assignment.provider.Endpoint),
			Model:            assignment.model,
			Dimensions:       int32(assignment.dimensions),
			State:            generationStateBuilding,
		})
		if err != nil {
			return nil, errors.Wrap(err, "failed to create index generation")
		}
	}
	if generation.State == generationStateRetired {
		generation.State = generationStateBuilding
		generation.RetiredTs = 0
		generation, err = ix.store.UpsertAIIndexGeneration(ctx, generation)
		if err != nil {
			return nil, errors.Wrap(err, "failed to resume retired generation")
		}
	}
	return generation, nil
}

// syncDocument re-chunks and re-embeds one document when its chunks are
// missing or built from another memo revision, honoring the memo's backoff.
// It reports whether the document's chunks are current afterwards. Failures
// back the memo off individually, so one poison memo cannot starve the
// corpus.
func (ix *Indexer) syncDocument(ctx context.Context, generation *store.AIIndexGeneration, assignment *embeddingAssignment, model internalai.Model, document *store.AISearchDocument) bool {
	if !ix.retryAllowed(document.MemoID) {
		return false
	}
	current, err := ix.syncDocumentOnce(ctx, generation, assignment, model, document)
	ix.recordResult(document.MemoID, err)
	return err == nil && current
}

// syncDocumentOnce embeds and commits every chunk of one document in bounded
// batches. It reports whether the document's chunks are current afterwards:
// a source revision that changed mid-pass commits nothing and leaves the
// document stale for the next signal or sweep rather than backing off.
func (ix *Indexer) syncDocumentOnce(ctx context.Context, generation *store.AIIndexGeneration, assignment *embeddingAssignment, model internalai.Model, document *store.AISearchDocument) (bool, error) {
	chunks := chunkDocument(document)
	current, err := ix.hasCurrentChunks(ctx, generation.ID, document, len(chunks))
	if err != nil {
		return false, err
	}
	if current {
		return true, nil
	}
	if len(chunks) == 0 {
		// Nothing to embed; the document is covered. Chunks left from a
		// superseded revision go now — there is no embed to wait for.
		if err := ix.pruneSupersededChunks(ctx, generation.ID, document); err != nil {
			return false, err
		}
		return true, nil
	}

	for start := 0; start < len(chunks); start += indexEmbedBatchSize {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		batch := chunks[start:min(start+indexEmbedBatchSize, len(chunks))]
		inputs := make([]string, 0, len(batch))
		for _, chunk := range batch {
			inputs = append(inputs, chunk.text)
		}
		response, err := model.Embed(ctx, internalai.EmbeddingRequest{
			Model:      assignment.model,
			Inputs:     inputs,
			Dimensions: assignment.dimensions,
		})
		if err != nil {
			ix.recordError(ctx, generation, err)
			return false, err
		}
		committed, err := ix.commitBatch(ctx, generation, assignment, document, batch, response)
		if err != nil {
			ix.recordError(ctx, generation, err)
			return false, err
		}
		if !committed {
			// The memo changed or left the corpus while its chunks were
			// embedding: discard this pass's vectors and leave the document
			// to the next signal or sweep. The superseded chunks keep
			// serving until the fresh set commits.
			return false, nil
		}
	}

	// The document's fresh chunks are all committed; only now drop chunks
	// built from superseded memo revisions. Keeping them until here means a
	// failed re-embed never leaves the serving generation without the
	// document's chunks.
	if err := ix.pruneSupersededChunks(ctx, generation.ID, document); err != nil {
		return false, err
	}
	return true, nil
}

// hasCurrentChunks reports whether the generation already holds exactly the
// chunks the deterministic chunker derives for the document's current memo
// revision and content hash: the same count, every one built from the current
// revision and hash. The hash catches a document rewritten without a
// revision bump, which the revision alone would miss.
func (ix *Indexer) hasCurrentChunks(ctx context.Context, generationID int32, document *store.AISearchDocument, expectedChunks int) (bool, error) {
	chunks, err := ix.store.ListAIIndexChunks(ctx, &store.FindAIIndexChunk{GenerationID: &generationID, MemoID: &document.MemoID})
	if err != nil {
		return false, errors.Wrap(err, "failed to list index chunks")
	}
	if len(chunks) != expectedChunks {
		return false, nil
	}
	for _, chunk := range chunks {
		if chunk.MemoRevision != document.MemoUpdatedTs || chunk.ContentHash != document.ContentHash {
			return false, nil
		}
	}
	return true, nil
}

// pruneSupersededChunks removes the document's chunks built from any memo
// revision or content hash other than its current ones, so a stale chunk
// never maps a hit to an outdated source span. It runs only after the
// document's fresh chunks committed (or when the document needs none): until
// then the superseded chunks keep serving, and the citation reread keeps
// their snippets honest.
func (ix *Indexer) pruneSupersededChunks(ctx context.Context, generationID int32, document *store.AISearchDocument) error {
	chunks, err := ix.store.ListAIIndexChunks(ctx, &store.FindAIIndexChunk{GenerationID: &generationID, MemoID: &document.MemoID})
	if err != nil {
		return errors.Wrap(err, "failed to list index chunks")
	}
	for _, chunk := range chunks {
		if chunk.MemoRevision == document.MemoUpdatedTs && chunk.ContentHash == document.ContentHash {
			continue
		}
		if err := ix.store.DeleteAIIndexChunk(ctx, &store.DeleteAIIndexChunk{ID: &chunk.ID}); err != nil {
			return errors.Wrap(err, "failed to drop stale index chunk")
		}
	}
	return nil
}

// commitBatch validates and stores one embedded batch of a document's
// chunks. Every vector must be finite, match the resolved dimensions, and
// encode to exactly 4 bytes per dimension; any mismatch rejects the whole
// batch before the first commit. All vectors are L2-normalized before
// encoding so query-time cosine similarity is a dot product. The batch
// commits only if the document's source revision and content hash still
// match the ones the chunks were built from; it reports whether the commit
// happened.
func (ix *Indexer) commitBatch(ctx context.Context, generation *store.AIIndexGeneration, assignment *embeddingAssignment, document *store.AISearchDocument, batch []documentChunk, response internalai.EmbeddingResponse) (bool, error) {
	expected := int32(assignment.dimensions)
	if expected == 0 {
		expected = int32(response.Dimensions)
	}
	if len(response.Vectors) != len(batch) {
		return false, errors.Errorf("expected %d vectors, got %d", len(batch), len(response.Vectors))
	}
	encoded := make([][]byte, len(batch))
	for i, vector := range response.Vectors {
		if int32(len(vector)) != expected {
			return false, errors.Errorf("vector %d has %d dimensions, expected %d", i, len(vector), expected)
		}
		normalized, err := internalai.NormalizeVector(vector)
		if err != nil {
			return false, errors.Wrapf(err, "vector %d is invalid", i)
		}
		encoded[i] = internalai.EncodeVector(normalized)
	}

	// Commit only if the source revision and content hash still match the
	// ones embedded.
	latest, err := ix.store.GetAISearchDocument(ctx, &store.FindAISearchDocument{MemoID: &document.MemoID})
	if err != nil {
		return false, errors.Wrap(err, "failed to recheck search document")
	}
	if latest == nil || latest.MemoUpdatedTs != document.MemoUpdatedTs || latest.ContentHash != document.ContentHash {
		return false, nil
	}

	for i, chunk := range batch {
		if _, err := ix.store.UpsertAIIndexChunk(ctx, &store.AIIndexChunk{
			GenerationID: generation.ID,
			MemoID:       document.MemoID,
			MemoRevision: document.MemoUpdatedTs,
			ChunkOrdinal: chunk.ordinal,
			ContentStart: chunk.contentStart,
			ContentEnd:   chunk.contentEnd,
			SourceStart:  chunk.sourceStart,
			SourceEnd:    chunk.sourceEnd,
			Vector:       encoded[i],
			Dimensions:   expected,
			ContentHash:  document.ContentHash,
		}); err != nil {
			return false, errors.Wrap(err, "failed to upsert index chunk")
		}
	}
	if generation.Dimensions == 0 {
		// The configured dimensions were the provider default; the first
		// successful batch resolves them.
		generation.Dimensions = expected
	}
	return true, nil
}

// retryAllowed reports whether the memo's indexing may run now under its
// backoff. Callers hold ix.mu.
func (ix *Indexer) retryAllowed(memoID int32) bool {
	return ix.backoff.allowed(memoID)
}

// recordResult clears the memo's backoff on success and backs off
// exponentially on failure, so a poison memo cannot stall reconciliation.
// Callers hold ix.mu.
func (ix *Indexer) recordResult(memoID int32, err error) {
	f := ix.backoff.record(memoID, err)
	if f != nil {
		slog.Warn("index chunk sync failed; backing off",
			slog.Int64("memo_id", int64(memoID)),
			slog.Int("failures", f.count),
			slog.Time("next_retry", f.nextRetry),
			slog.Any("err", err))
	}
}

// recordError persists a normalized error category on the generation: never
// raw query text, memo text, provider payloads, or endpoint secrets.
func (ix *Indexer) recordError(ctx context.Context, generation *store.AIIndexGeneration, cause error) {
	generation.LastError = string(internalai.CategoryOf(cause))
	if _, err := ix.store.UpsertAIIndexGeneration(ctx, generation); err != nil {
		// The pass continues; the error category surfaces on the generation
		// once it can be written.
		_ = err
	}
}
