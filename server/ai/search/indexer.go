package search

import (
	"context"

	"github.com/pkg/errors"

	internalai "github.com/usememos/memos/internal/ai"
	"github.com/usememos/memos/internal/ai/gateway"
	"github.com/usememos/memos/store"
)

// Generation lifecycle states. Scaffold C treats the single generation as
// active once its one-shot pass completes; there is no BUILDING/ACTIVE
// cutover, retirement, or grace period yet.
// TODO(issue 04): replace Scaffold C with the full generation lifecycle and
// atomic cutover.
const (
	// generationStateBuilding marks a generation whose indexing pass has not
	// completed. A building generation never serves queries.
	generationStateBuilding = "BUILDING"
	// generationStateActive marks the generation semantic queries scan.
	generationStateActive = "ACTIVE"
	// The RETIRED state is reserved for the cutover lifecycle (issue 04);
	// Scaffold C never sets it.
)

const (
	// scaffoldIndexMaxDocuments bounds the documents one indexing pass
	// covers.
	// TODO(issue 03): replace Scaffold B with runner-integrated
	// reconciliation, resumable sweeps, and deletion cleanup.
	scaffoldIndexMaxDocuments = 512
	// indexScanBatchSize bounds every search-document read of the pass.
	indexScanBatchSize = 100
	// indexEmbedBatchSize bounds every embedding call of the pass.
	indexEmbedBatchSize = 16
)

// Indexer builds embedding index generations over the search documents.
// Scaffold B: a manually triggered, bounded one-shot pass through the
// Stage 1 Embed interface; there is no runner integration and no
// reconciliation yet.
// TODO(issue 03): replace Scaffold B with runner-integrated reconciliation.
type Indexer struct {
	store        *store.Store
	modelFactory func() gateway.ModelFactory
}

// NewIndexer creates the indexing pass runner. The model factory indirection
// lets tests swap the provider adapters after construction.
func NewIndexer(s *store.Store, modelFactory func() gateway.ModelFactory) *Indexer {
	return &Indexer{store: s, modelFactory: modelFactory}
}

// factory returns the configured model factory, or nil for the production
// adapters.
func (ix *Indexer) factory() gateway.ModelFactory {
	if ix.modelFactory == nil {
		return nil
	}
	return ix.modelFactory()
}

// pendingChunk is one derived chunk of a search document awaiting its
// embedding vector.
type pendingChunk struct {
	document *store.AISearchDocument
	chunk    documentChunk
}

// RunOnce runs one bounded, manually triggered indexing pass over the
// current search documents (Scaffold B). With no embedding assignment it
// does nothing. Documents whose chunks already match their current revision
// are skipped, so a repeated pass is idempotent. When the pass completes,
// the generation is treated as active (Scaffold C).
func (ix *Indexer) RunOnce(ctx context.Context) error {
	setting, err := ix.store.GetInstanceAISetting(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to get AI setting")
	}
	assignment := resolveEmbeddingAssignment(setting)
	if assignment == nil {
		// No embedding capability configured: no semantic path executes.
		return nil
	}

	fingerprint := assignment.indexFingerprint()
	generation, err := ix.store.GetAIIndexGeneration(ctx, &store.FindAIIndexGeneration{Fingerprint: &fingerprint})
	if err != nil {
		return errors.Wrap(err, "failed to find index generation")
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
			return errors.Wrap(err, "failed to create index generation")
		}
	}

	model, err := assignment.resolveModel(ix.factory())
	if err != nil {
		return errors.Wrap(err, "failed to resolve embedding model")
	}

	pending, total, indexed, err := ix.collectPending(ctx, generation)
	if err != nil {
		return err
	}

	// remaining counts each pending document's chunks not yet committed, so a
	// document counts as indexed only once every chunk of it committed.
	remaining := map[int32]int{}
	for _, p := range pending {
		remaining[p.document.MemoID]++
	}

	for start := 0; start < len(pending); start += indexEmbedBatchSize {
		if err := ctx.Err(); err != nil {
			return err
		}
		batch := pending[start:min(start+indexEmbedBatchSize, len(pending))]
		inputs := make([]string, 0, len(batch))
		for _, p := range batch {
			inputs = append(inputs, p.chunk.text)
		}
		response, err := model.Embed(ctx, internalai.EmbeddingRequest{
			Model:      assignment.model,
			Inputs:     inputs,
			Dimensions: assignment.dimensions,
		})
		if err != nil {
			ix.recordError(ctx, generation, err)
			continue
		}
		if err := ix.commitBatch(ctx, generation, assignment, batch, response); err != nil {
			ix.recordError(ctx, generation, err)
			continue
		}
		for _, p := range batch {
			remaining[p.document.MemoID]--
			if remaining[p.document.MemoID] == 0 {
				indexed++
				// The document's fresh chunks are all committed; only now
				// drop chunks built from superseded memo revisions. Keeping
				// them until here means a failed re-embed never leaves the
				// serving generation without the document's chunks.
				if err := ix.pruneSupersededChunks(ctx, generation.ID, p.document); err != nil {
					return err
				}
			}
		}
	}

	generation.MemoTotal = int32(total)
	generation.MemoIndexed = int32(indexed)
	if indexed == total {
		// The pass covered every eligible document, so the generation may
		// serve queries. A pass with rejected batches stays building: a
		// partial index never serves queries.
		generation.State = generationStateActive
	}
	if _, err := ix.store.UpsertAIIndexGeneration(ctx, generation); err != nil {
		return errors.Wrap(err, "failed to update index generation")
	}
	return nil
}

// collectPending walks the current search documents in ascending ID order up
// to the scaffold bound, counting the eligible documents and collecting the
// chunks of the ones whose chunks are missing or built from another memo
// revision.
func (ix *Indexer) collectPending(ctx context.Context, generation *store.AIIndexGeneration) ([]pendingChunk, int, int, error) {
	pending := []pendingChunk{}
	total, indexed := 0, 0
	var afterID int32
	for total < scaffoldIndexMaxDocuments {
		if err := ctx.Err(); err != nil {
			return nil, 0, 0, err
		}
		limit := indexScanBatchSize
		documents, err := ix.store.ListAISearchDocuments(ctx, &store.FindAISearchDocument{IDGreaterThan: &afterID, Limit: &limit})
		if err != nil {
			return nil, 0, 0, errors.Wrap(err, "failed to list search documents")
		}
		for _, document := range documents {
			afterID = document.ID
			if total >= scaffoldIndexMaxDocuments {
				break
			}
			if document.ProjectionVersion != ProjectionVersion || document.NormalizationVersion != NormalizationVersion {
				continue
			}
			total++
			chunks := chunkDocument(document)
			current, err := ix.hasCurrentChunks(ctx, generation.ID, document, len(chunks))
			if err != nil {
				return nil, 0, 0, err
			}
			if current {
				indexed++
				continue
			}
			if len(chunks) == 0 {
				// Nothing to embed; the document is covered. Chunks left from
				// a superseded revision go now — there is no embed to wait for.
				if err := ix.pruneSupersededChunks(ctx, generation.ID, document); err != nil {
					return nil, 0, 0, err
				}
				indexed++
				continue
			}
			for _, chunk := range chunks {
				pending = append(pending, pendingChunk{document: document, chunk: chunk})
			}
		}
		if len(documents) < limit {
			break
		}
	}
	return pending, total, indexed, nil
}

// hasCurrentChunks reports whether the generation already holds exactly the
// chunks the deterministic chunker derives for the document's current memo
// revision: the same count, every one built from the current revision.
func (ix *Indexer) hasCurrentChunks(ctx context.Context, generationID int32, document *store.AISearchDocument, expectedChunks int) (bool, error) {
	chunks, err := ix.store.ListAIIndexChunks(ctx, &store.FindAIIndexChunk{GenerationID: &generationID, MemoID: &document.MemoID})
	if err != nil {
		return false, errors.Wrap(err, "failed to list index chunks")
	}
	if len(chunks) != expectedChunks {
		return false, nil
	}
	for _, chunk := range chunks {
		if chunk.MemoRevision != document.MemoUpdatedTs {
			return false, nil
		}
	}
	return true, nil
}

// pruneSupersededChunks removes the document's chunks built from any memo
// revision other than its current one, so a stale chunk never maps a hit to
// an outdated source span. It runs only after the document's fresh chunks
// committed (or when the document needs none): until then the superseded
// chunks keep serving, and the citation reread keeps their snippets honest.
func (ix *Indexer) pruneSupersededChunks(ctx context.Context, generationID int32, document *store.AISearchDocument) error {
	chunks, err := ix.store.ListAIIndexChunks(ctx, &store.FindAIIndexChunk{GenerationID: &generationID, MemoID: &document.MemoID})
	if err != nil {
		return errors.Wrap(err, "failed to list index chunks")
	}
	for _, chunk := range chunks {
		if chunk.MemoRevision == document.MemoUpdatedTs {
			continue
		}
		if err := ix.store.DeleteAIIndexChunk(ctx, &store.DeleteAIIndexChunk{ID: &chunk.ID}); err != nil {
			return errors.Wrap(err, "failed to drop stale index chunk")
		}
	}
	return nil
}

// commitBatch validates and stores one embedded batch of chunks. Every
// vector must be finite, match the resolved dimensions, and encode to
// exactly 4 bytes per dimension; any mismatch rejects the whole batch before
// the first commit. All vectors are L2-normalized before encoding so
// query-time cosine similarity is a dot product.
func (ix *Indexer) commitBatch(ctx context.Context, generation *store.AIIndexGeneration, assignment *embeddingAssignment, batch []pendingChunk, response internalai.EmbeddingResponse) error {
	expected := int32(assignment.dimensions)
	if expected == 0 {
		expected = int32(response.Dimensions)
	}
	if len(response.Vectors) != len(batch) {
		return errors.Errorf("expected %d vectors, got %d", len(batch), len(response.Vectors))
	}
	encoded := make([][]byte, len(batch))
	for i, vector := range response.Vectors {
		if int32(len(vector)) != expected {
			return errors.Errorf("vector %d has %d dimensions, expected %d", i, len(vector), expected)
		}
		normalized, err := internalai.NormalizeVector(vector)
		if err != nil {
			return errors.Wrapf(err, "vector %d is invalid", i)
		}
		encoded[i] = internalai.EncodeVector(normalized)
	}

	for i, p := range batch {
		chunk := p.chunk
		if _, err := ix.store.UpsertAIIndexChunk(ctx, &store.AIIndexChunk{
			GenerationID: generation.ID,
			MemoID:       p.document.MemoID,
			MemoRevision: p.document.MemoUpdatedTs,
			ChunkOrdinal: chunk.ordinal,
			ContentStart: chunk.contentStart,
			ContentEnd:   chunk.contentEnd,
			SourceStart:  chunk.sourceStart,
			SourceEnd:    chunk.sourceEnd,
			Vector:       encoded[i],
			Dimensions:   expected,
		}); err != nil {
			return errors.Wrap(err, "failed to upsert index chunk")
		}
	}
	if generation.Dimensions == 0 {
		// The configured dimensions were the provider default; the first
		// successful batch resolves them.
		generation.Dimensions = expected
	}
	return nil
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
