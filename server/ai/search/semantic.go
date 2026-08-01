package search

import (
	"cmp"
	"context"
	"log/slog"
	"slices"

	"github.com/pkg/errors"

	internalai "github.com/usememos/memos/internal/ai"
	"github.com/usememos/memos/internal/ai/gateway"
	storepb "github.com/usememos/memos/proto/gen/store"
	"github.com/usememos/memos/store"
)

const (
	// semanticScanBatchSize bounds every chunk-vector read of the semantic
	// scan.
	semanticScanBatchSize = 512
	// semanticMaxCandidates caps the semantic candidates fused into one query.
	semanticMaxCandidates = 50
)

// RankReasonSemantic marks a result the semantic path produced or
// corroborated.
const RankReasonSemantic = "semantic"

// embeddingAssignment is the configured embedding capability resolved from
// the instance AI setting.
type embeddingAssignment struct {
	provider   internalai.ProviderConfig
	model      string
	dimensions int
}

// resolveEmbeddingAssignment returns the configured embedding assignment, or
// nil when no embedding capability is assigned or the assigned provider is
// gone. With no assignment every Stage 2 behavior is unchanged and no
// semantic path executes.
func resolveEmbeddingAssignment(setting *storepb.InstanceAISetting) *embeddingAssignment {
	embedding := setting.GetEmbedding()
	if embedding.GetProviderId() == "" || embedding.GetModel() == "" {
		return nil
	}
	provider, err := internalai.FindProvider(convertAIProviders(setting.GetProviders()), embedding.GetProviderId())
	if err != nil {
		return nil
	}
	return &embeddingAssignment{
		provider:   *provider,
		model:      embedding.GetModel(),
		dimensions: int(embedding.GetDimensions()),
	}
}

// indexFingerprint computes the desired generation fingerprint for the
// assignment under the current projection, chunker, normalization, and
// vector-encoding versions. The dimensions are the configured ones; when the
// assignment uses the provider default (0), the generation records the
// resolved dimensions after its first batch and the query path's dimension
// isolation check keeps mismatched vectors from mixing.
func (a *embeddingAssignment) indexFingerprint() string {
	return computeIndexFingerprint(
		a.provider.Type,
		endpointIdentity(a.provider.Endpoint),
		a.model,
		int32(a.dimensions),
	)
}

// resolveModel builds the callable embedding model for the assignment
// through the hardened transport. A nil factory falls back to the production
// adapters.
func (a *embeddingAssignment) resolveModel(factory gateway.ModelFactory) (internalai.Model, error) {
	if factory == nil {
		factory = gateway.NewModel
	}
	return factory(a.provider, internalai.NewHTTPClient(internalai.TransportConfig{
		AllowPrivateNetwork: a.provider.AllowPrivateNetwork,
	}))
}

// convertAIProviders maps stored provider configs to provider-neutral
// configs. It mirrors convertProviders in server/ai, which this package
// cannot import without an import cycle.
func convertAIProviders(providers []*storepb.AIProviderConfig) []internalai.ProviderConfig {
	converted := make([]internalai.ProviderConfig, 0, len(providers))
	for _, provider := range providers {
		if provider == nil {
			continue
		}
		converted = append(converted, internalai.ProviderConfig{
			ID:                  provider.GetId(),
			Title:               provider.GetTitle(),
			Type:                convertAIProviderType(provider.GetType()),
			Endpoint:            provider.GetEndpoint(),
			APIKey:              provider.GetApiKey(),
			AllowPrivateNetwork: provider.GetAllowPrivateNetwork(),
		})
	}
	return converted
}

// convertAIProviderType maps the stored provider type to the provider-neutral
// type.
func convertAIProviderType(providerType storepb.AIProviderType) internalai.ProviderType {
	switch providerType {
	case storepb.AIProviderType_OPENAI:
		return internalai.ProviderOpenAI
	case storepb.AIProviderType_GEMINI:
		return internalai.ProviderGemini
	default:
		return ""
	}
}

// SemanticSearcher embeds the query through the configured embedding
// capability and cosine-compares it against the active generation's chunks
// in Go over bounded batches.
type SemanticSearcher struct {
	store        *store.Store
	modelFactory func() gateway.ModelFactory
}

// NewSemanticSearcher creates the semantic query path. The model factory
// indirection lets tests swap the provider adapters after construction.
func NewSemanticSearcher(s *store.Store, modelFactory func() gateway.ModelFactory) *SemanticSearcher {
	return &SemanticSearcher{store: s, modelFactory: modelFactory}
}

// factory returns the configured model factory, or nil for the production
// adapters.
func (s *SemanticSearcher) factory() gateway.ModelFactory {
	if s.modelFactory == nil {
		return nil
	}
	return s.modelFactory()
}

// semanticMatch is one memo ranked by the semantic scan: its best chunk's
// cosine similarity, the chunk's start within the projected content, and the
// source revision and content hash the chunk was built from, so callers can
// drop matches built from a superseded document.
type semanticMatch struct {
	memoID       int32
	score        float64
	contentPos   int32
	memoRevision int64
	contentHash  string
}

// Search returns the memos whose chunks are nearest to the query text in the
// active generation, best first. Matches built from a superseded source
// revision or content hash are dropped, so stale indexed text never serves.
// It returns nil when semantic retrieval is not callable: no embedding
// assignment, no active generation matching the current fingerprint, or a
// dimension mismatch between the query vector and the generation. An error
// means the semantic path failed; callers degrade to lexical results.
func (s *SemanticSearcher) Search(ctx context.Context, queryText string) ([]semanticMatch, error) {
	setting, err := s.store.GetInstanceAISetting(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get AI setting")
	}
	assignment := resolveEmbeddingAssignment(setting)
	if assignment == nil {
		return nil, nil
	}

	fingerprint := assignment.indexFingerprint()
	activeState := generationStateActive
	generation, err := s.store.GetAIIndexGeneration(ctx, &store.FindAIIndexGeneration{
		Fingerprint: &fingerprint,
		State:       &activeState,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to find index generation")
	}
	if generation == nil {
		return nil, nil
	}

	model, err := assignment.resolveModel(s.factory())
	if err != nil {
		return nil, errors.Wrap(err, "failed to resolve embedding model")
	}
	response, err := model.Embed(ctx, internalai.EmbeddingRequest{
		Model:      assignment.model,
		Inputs:     []string{queryText},
		Dimensions: assignment.dimensions,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to embed query")
	}
	if len(response.Vectors) != 1 {
		return nil, errors.Errorf("expected one query vector, got %d", len(response.Vectors))
	}
	queryVector, err := internalai.NormalizeVector(response.Vectors[0])
	if err != nil {
		return nil, errors.Wrap(err, "query vector is invalid")
	}
	if generation.Dimensions > 0 && int32(len(queryVector)) != generation.Dimensions {
		// Dimension isolation: the generation is not callable with the
		// current embedding configuration.
		return nil, nil
	}

	best := map[int32]*semanticMatch{}
	var afterID int32
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		limit := semanticScanBatchSize
		chunks, err := s.store.ListAIIndexChunks(ctx, &store.FindAIIndexChunk{
			GenerationID:  &generation.ID,
			IDGreaterThan: &afterID,
			Limit:         &limit,
		})
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan index chunks")
		}
		for _, chunk := range chunks {
			afterID = chunk.ID
			vector, err := internalai.DecodeVector(chunk.Vector, int(chunk.Dimensions))
			if err != nil || len(vector) != len(queryVector) {
				continue
			}
			score := dot(queryVector, vector)
			current, ok := best[chunk.MemoID]
			if !ok || score > current.score {
				best[chunk.MemoID] = &semanticMatch{memoID: chunk.MemoID, score: score, contentPos: chunk.ContentStart, memoRevision: chunk.MemoRevision, contentHash: chunk.ContentHash}
			}
		}
		if len(chunks) < limit {
			break
		}
	}

	matches := make([]semanticMatch, 0, len(best))
	for _, match := range best {
		matches = append(matches, *match)
	}
	sortSemanticMatches(matches)
	if len(matches) > semanticMaxCandidates {
		matches = matches[:semanticMaxCandidates]
	}

	// Staleness guard: a chunk built from a superseded source revision or
	// content hash never serves — the memo returns once the indexer catches
	// up. The citation reread remains the last line of defense.
	current := make([]semanticMatch, 0, len(matches))
	for _, match := range matches {
		document, err := s.store.GetAISearchDocument(ctx, &store.FindAISearchDocument{MemoID: &match.memoID})
		if err != nil {
			return nil, errors.Wrap(err, "failed to recheck search document")
		}
		if document == nil || document.MemoUpdatedTs != match.memoRevision || document.ContentHash != match.contentHash {
			continue
		}
		current = append(current, match)
	}
	return current, nil
}

// dot returns the dot product of two equal-length vectors. Both vectors are
// L2-normalized at storage and query time, so the dot product is the cosine
// similarity.
func dot(a, b []float32) float64 {
	sum := 0.0
	for i := range a {
		sum += float64(a[i]) * float64(b[i])
	}
	return sum
}

// sortSemanticMatches orders matches by similarity descending, then by memo
// ID ascending for determinism.
func sortSemanticMatches(matches []semanticMatch) {
	slices.SortFunc(matches, func(a, b semanticMatch) int {
		if a.score != b.score {
			return cmp.Compare(b.score, a.score)
		}
		return cmp.Compare(a.memoID, b.memoID)
	})
}

// fuseSemantic merges the semantic matches into the lexical candidates with
// naive interleaving: lexical and semantic candidates alternate, a memo in
// both keeps its lexical position and gains the semantic rank reason, and
// the merged list stays within the candidate budget.
// TODO(issue 05): replace Scaffold D with ordinal-rank fusion, per-path rank
// reasons, and the semantic scan budget.
func (r *Retriever) fuseSemantic(ctx context.Context, queryText string, tagFilters []string, lexical []candidate, budgets Budgets) []candidate {
	if r.semantic == nil {
		return lexical
	}
	matches, err := r.semantic.Search(ctx, queryText)
	if err != nil {
		// Semantic failure never breaks search: lexical results serve the
		// query unchanged.
		slog.WarnContext(ctx, "semantic retrieval unavailable", "error", err)
		return lexical
	}
	if len(matches) == 0 {
		return lexical
	}

	lexicalByMemo := make(map[int32]int, len(lexical))
	for i, c := range lexical {
		lexicalByMemo[c.document.MemoID] = i
	}

	semantic := []candidate{}
	for _, match := range matches {
		if index, ok := lexicalByMemo[match.memoID]; ok {
			// A match built from a superseded document adds nothing: the memo
			// keeps its lexical hit but not the semantic corroboration.
			if matchCurrent(lexical[index].document, match) {
				lexical[index].reasons = appendReason(lexical[index].reasons, RankReasonSemantic)
			}
			continue
		}
		document, err := r.store.GetAISearchDocument(ctx, &store.FindAISearchDocument{MemoID: &match.memoID})
		if err != nil || document == nil {
			continue
		}
		if !matchCurrent(document, match) {
			// The winning chunk was built from a superseded revision or hash:
			// stale indexed text never serves; the memo returns once the
			// indexer catches up.
			continue
		}
		if document.ProjectionVersion != ProjectionVersion || document.NormalizationVersion != NormalizationVersion {
			continue
		}
		if !tagsMatch(document.Tags, tagFilters) {
			continue
		}
		semantic = append(semantic, candidate{
			document: document,
			score:    match.score,
			reasons:  []string{RankReasonSemantic},
			anchor:   semanticAnchor(document, match.contentPos),
		})
	}
	return mergeInterleaved(lexical, semantic, budgets)
}

// matchCurrent reports whether a semantic match was built from the document's
// current source revision and content hash.
func matchCurrent(document *store.AISearchDocument, match semanticMatch) bool {
	return document.MemoUpdatedTs == match.memoRevision && document.ContentHash == match.contentHash
}

// mergeInterleaved merges lexical and semantic candidates with naive
// interleaving: the two lists alternate and the merged list stays within the
// candidate budget.
// TODO(issue 05): replaced together with the fusion caller by ordinal-rank
// fusion.
func mergeInterleaved(lexical, semantic []candidate, budgets Budgets) []candidate {
	merged := make([]candidate, 0, min(len(lexical)+len(semantic), budgets.MaxCandidates))
	lexicalIndex, semanticIndex := 0, 0
	for len(merged) < budgets.MaxCandidates && (lexicalIndex < len(lexical) || semanticIndex < len(semantic)) {
		if lexicalIndex < len(lexical) {
			merged = append(merged, lexical[lexicalIndex])
			lexicalIndex++
			if len(merged) >= budgets.MaxCandidates {
				break
			}
		}
		if semanticIndex < len(semantic) {
			merged = append(merged, semantic[semanticIndex])
			semanticIndex++
		}
	}
	return merged
}

// semanticAnchor returns the first content token at or after the chunk's
// start within the projected content, used to place the snippet near the
// matching chunk.
func semanticAnchor(document *store.AISearchDocument, contentPos int32) string {
	if contentPos < 0 || int(contentPos) >= len(document.Content) {
		return ""
	}
	words := tokenize(document.Content[contentPos:])
	if len(words) == 0 {
		return ""
	}
	return words[0]
}
