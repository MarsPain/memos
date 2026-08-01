package test

import (
	"context"
	"hash/fnv"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	internalai "github.com/usememos/memos/internal/ai"
	"github.com/usememos/memos/internal/ai/aitest"
	v1pb "github.com/usememos/memos/proto/gen/api/v1"
	storepb "github.com/usememos/memos/proto/gen/store"
	"github.com/usememos/memos/store"
)

func TestSearchMemos(t *testing.T) {
	ctx := context.Background()

	t.Run("requires authentication", func(t *testing.T) {
		ts := NewTestService(t)
		defer ts.Cleanup()

		_, err := ts.Service.SearchMemos(ctx, &v1pb.SearchMemosRequest{Query: "owl"})
		require.Equal(t, codes.Unauthenticated, status.Code(err))
	})

	t.Run("returns ranked results with citations and works without generation configuration", func(t *testing.T) {
		ts := NewTestService(t)
		defer ts.Cleanup()

		user, err := ts.CreateRegularUser(ctx, "alice")
		require.NoError(t, err)
		userCtx := ts.CreateUserContext(ctx, user.ID)

		_, err = ts.Store.CreateMemo(ctx, &store.Memo{UID: "birding", CreatorID: user.ID, Content: "# Birding\n\nThe owl hunts at dusk. #birds", Visibility: store.Private})
		require.NoError(t, err)
		_, err = ts.Store.CreateMemo(ctx, &store.Memo{UID: "bats", CreatorID: user.ID, Content: "Bats navigate by echo.", Visibility: store.Private})
		require.NoError(t, err)
		// The search documents are maintained outside the write path; close
		// the index lag for the memos just written.
		ts.Service.SearchService().RunOnce(ctx)

		// No generation or embedding is configured: search still works.
		response, err := ts.Service.SearchMemos(userCtx, &v1pb.SearchMemosRequest{Query: "owl"})
		require.NoError(t, err)
		require.Empty(t, response.GetPartialReasons())
		require.Len(t, response.GetResults(), 1)
		result := response.GetResults()[0]
		require.Equal(t, "memos/birding", result.GetMemo())
		require.Contains(t, result.GetSnippet(), "owl")
		require.Contains(t, result.GetRankReasons(), "content_exact")
	})

	t.Run("never returns another user's private memo", func(t *testing.T) {
		ts := NewTestService(t)
		defer ts.Cleanup()

		alice, err := ts.CreateRegularUser(ctx, "alice")
		require.NoError(t, err)
		bob, err := ts.CreateRegularUser(ctx, "bob")
		require.NoError(t, err)
		bobCtx := ts.CreateUserContext(ctx, bob.ID)

		_, err = ts.Store.CreateMemo(ctx, &store.Memo{UID: "alice-secret", CreatorID: alice.ID, Content: "The owl hunts at dusk.", Visibility: store.Private})
		require.NoError(t, err)
		ts.Service.SearchService().RunOnce(ctx)

		response, err := ts.Service.SearchMemos(bobCtx, &v1pb.SearchMemosRequest{Query: "owl"})
		require.NoError(t, err)
		require.Empty(t, response.GetResults())
	})

	t.Run("translates structured filters and rejects invalid creator names", func(t *testing.T) {
		ts := NewTestService(t)
		defer ts.Cleanup()

		alice, err := ts.CreateRegularUser(ctx, "alice")
		require.NoError(t, err)
		aliceCtx := ts.CreateUserContext(ctx, alice.ID)
		bob, err := ts.CreateRegularUser(ctx, "bob")
		require.NoError(t, err)

		_, err = ts.Store.CreateMemo(ctx, &store.Memo{UID: "alice-owl", CreatorID: alice.ID, Content: "The owl hunts. #birds", Visibility: store.Private})
		require.NoError(t, err)
		_, err = ts.Store.CreateMemo(ctx, &store.Memo{UID: "bob-owl", CreatorID: bob.ID, Content: "The owl sleeps.", Visibility: store.Public})
		require.NoError(t, err)
		ts.Service.SearchService().RunOnce(ctx)

		// Tag and visibility filters narrow the candidates.
		response, err := ts.Service.SearchMemos(aliceCtx, &v1pb.SearchMemosRequest{
			Query:  "owl",
			Filter: &v1pb.SearchMemosFilter{Tags: []string{"birds"}},
		})
		require.NoError(t, err)
		require.Len(t, response.GetResults(), 1)
		require.Equal(t, "memos/alice-owl", response.GetResults()[0].GetMemo())

		response, err = ts.Service.SearchMemos(aliceCtx, &v1pb.SearchMemosRequest{
			Query:  "owl",
			Filter: &v1pb.SearchMemosFilter{Visibility: v1pb.Visibility_PUBLIC},
		})
		require.NoError(t, err)
		require.Len(t, response.GetResults(), 1)
		require.Equal(t, "memos/bob-owl", response.GetResults()[0].GetMemo())

		// The creator filter takes a user resource name.
		response, err = ts.Service.SearchMemos(aliceCtx, &v1pb.SearchMemosRequest{
			Query:  "owl",
			Filter: &v1pb.SearchMemosFilter{Creator: "users/" + strconv.Itoa(int(bob.ID))},
		})
		require.NoError(t, err)
		require.Len(t, response.GetResults(), 1)
		require.Equal(t, "memos/bob-owl", response.GetResults()[0].GetMemo())

		_, err = ts.Service.SearchMemos(aliceCtx, &v1pb.SearchMemosRequest{
			Query:  "owl",
			Filter: &v1pb.SearchMemosFilter{Creator: "bob"},
		})
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("requires a query", func(t *testing.T) {
		ts := NewTestService(t)
		defer ts.Cleanup()

		user, err := ts.CreateRegularUser(ctx, "alice")
		require.NoError(t, err)
		userCtx := ts.CreateUserContext(ctx, user.ID)

		_, err = ts.Service.SearchMemos(userCtx, &v1pb.SearchMemosRequest{Query: "  "})
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	})
}

// semanticTestDimensions is the embedding size the semantic RPC test uses.
const semanticTestDimensions = 8

// semanticTestBuckets maps words to a shared semantic bucket so the
// deterministic fake treats bucket-sharing words as related without sharing
// a token.
var semanticTestBuckets = map[string]int{
	"owl":       0,
	"hunts":     0,
	"nocturnal": 0,
	"predator":  0,
	"kitchen":   1,
	"simmer":    1,
	"tomato":    1,
	"sauce":     1,
}

// bucketEmbedFunc deterministically embeds texts as bag-of-words vectors
// over the semantic buckets; unmapped words hash into a bucket.
func bucketEmbedFunc(request internalai.EmbeddingRequest) (internalai.EmbeddingResponse, error) {
	vectors := make([][]float32, 0, len(request.Inputs))
	for _, input := range request.Inputs {
		vector := make([]float32, semanticTestDimensions)
		for _, word := range strings.FieldsFunc(strings.ToLower(input), func(r rune) bool { return !unicode.IsLetter(r) }) {
			bucket, ok := semanticTestBuckets[word]
			if !ok {
				h := fnv.New32a()
				_, _ = h.Write([]byte(word))
				bucket = int(h.Sum32() % semanticTestDimensions)
			}
			vector[bucket]++
		}
		vectors = append(vectors, vector)
	}
	return internalai.EmbeddingResponse{Vectors: vectors, Dimensions: semanticTestDimensions}, nil
}

// configureSemanticEmbedding points the service's model factory at the fake
// model and assigns the embedding capability.
func configureSemanticEmbedding(ctx context.Context, t *testing.T, ts *TestService, model internalai.Model) {
	t.Helper()
	ts.Service.AIModelFactory = func(internalai.ProviderConfig, *http.Client) (internalai.Model, error) {
		return model, nil
	}
	_, err := ts.Store.UpsertInstanceSetting(ctx, &storepb.InstanceSetting{
		Key: storepb.InstanceSettingKey_AI,
		Value: &storepb.InstanceSetting_AiSetting{AiSetting: &storepb.InstanceAISetting{
			Providers: []*storepb.AIProviderConfig{{
				Id: "p", Title: "P", Type: storepb.AIProviderType_OPENAI,
				Endpoint: "https://embed.example.com/v1", ApiKey: "sk-test",
			}},
			Embedding: &storepb.EmbeddingConfig{ProviderId: "p", Model: "embed-model", Dimensions: semanticTestDimensions},
		}},
	})
	require.NoError(t, err)
}

func TestSearchMemosSemanticRetrieval(t *testing.T) {
	ctx := context.Background()

	t.Run("a meaning-based query returns the relevant memo in fused results", func(t *testing.T) {
		ts := NewTestService(t)
		defer ts.Cleanup()

		configureSemanticEmbedding(ctx, t, ts, &aitest.Model{EmbedFunc: bucketEmbedFunc})

		user, err := ts.CreateRegularUser(ctx, "alice")
		require.NoError(t, err)
		userCtx := ts.CreateUserContext(ctx, user.ID)

		_, err = ts.Store.CreateMemo(ctx, &store.Memo{UID: "owl", CreatorID: user.ID, Content: "# Field Notes\n\nThe owl hunts field mice at dusk.", Visibility: store.Private})
		require.NoError(t, err)
		_, err = ts.Store.CreateMemo(ctx, &store.Memo{UID: "cooking", CreatorID: user.ID, Content: "# Kitchen\n\nSimmer the tomato sauce slowly.", Visibility: store.Private})
		require.NoError(t, err)

		// The runner catches up on its own: the search-document refresh
		// drives the embedding indexer through the refresh hooks, with no
		// manual indexing trigger.
		ts.Service.SearchService().RunOnce(ctx)

		// The query shares no token with the memo, yet the memo ranks first
		// through the fused semantic path.
		response, err := ts.Service.SearchMemos(userCtx, &v1pb.SearchMemosRequest{Query: "nocturnal predator"})
		require.NoError(t, err)
		require.NotEmpty(t, response.GetResults())
		require.Equal(t, "memos/owl", response.GetResults()[0].GetMemo())
		require.Contains(t, response.GetResults()[0].GetRankReasons(), "semantic")
		require.NotEmpty(t, response.GetResults()[0].GetSnippet())
		require.NotEmpty(t, response.GetResults()[0].GetSourceHash())
	})

	t.Run("a memo written through the API reaches semantic results after the runner catches up", func(t *testing.T) {
		ts := NewTestService(t)
		defer ts.Cleanup()

		configureSemanticEmbedding(ctx, t, ts, &aitest.Model{EmbedFunc: bucketEmbedFunc})

		user, err := ts.CreateRegularUser(ctx, "alice")
		require.NoError(t, err)
		userCtx := ts.CreateUserContext(ctx, user.ID)

		// The memo write path only emits an invalidation signal; projection,
		// chunking, and embedding stay outside of it.
		created, err := ts.Service.CreateMemo(userCtx, &v1pb.CreateMemoRequest{
			Memo: &v1pb.Memo{
				Content:    "# Field Notes\n\nThe owl hunts field mice at dusk.",
				Visibility: v1pb.Visibility_PRIVATE,
			},
		})
		require.NoError(t, err)

		// The runner catches up with no manual trigger.
		ts.Service.SearchService().RunOnce(ctx)

		response, err := ts.Service.SearchMemos(userCtx, &v1pb.SearchMemosRequest{Query: "nocturnal predator"})
		require.NoError(t, err)
		require.NotEmpty(t, response.GetResults())
		require.Equal(t, created.GetName(), response.GetResults()[0].GetMemo())
		require.Contains(t, response.GetResults()[0].GetRankReasons(), "semantic")
	})

	t.Run("memo deletion removes derived chunks from every generation", func(t *testing.T) {
		ts := NewTestService(t)
		defer ts.Cleanup()

		configureSemanticEmbedding(ctx, t, ts, &aitest.Model{EmbedFunc: bucketEmbedFunc})

		user, err := ts.CreateRegularUser(ctx, "alice")
		require.NoError(t, err)
		userCtx := ts.CreateUserContext(ctx, user.ID)

		memo, err := ts.Store.CreateMemo(ctx, &store.Memo{UID: "owl", CreatorID: user.ID, Content: "# Field Notes\n\nThe owl hunts field mice at dusk.", Visibility: store.Private})
		require.NoError(t, err)

		ts.Service.SearchService().RunOnce(ctx)
		chunks, err := ts.Store.ListAIIndexChunks(ctx, &store.FindAIIndexChunk{MemoID: &memo.ID})
		require.NoError(t, err)
		require.NotEmpty(t, chunks)

		// A second generation also holds chunks of the memo: deletion must
		// cover every generation, not only the serving one.
		other, err := ts.Store.UpsertAIIndexGeneration(ctx, &store.AIIndexGeneration{
			Fingerprint: "other-fingerprint",
			ProviderID:  "p",
			Model:       "embed-model",
			Dimensions:  semanticTestDimensions,
			State:       "ACTIVE",
		})
		require.NoError(t, err)
		_, err = ts.Store.UpsertAIIndexChunk(ctx, &store.AIIndexChunk{
			GenerationID: other.ID,
			MemoID:       memo.ID,
			MemoRevision: memo.UpdatedTs,
			ChunkOrdinal: 0,
			Vector:       []byte{1, 2, 3, 4},
			Dimensions:   1,
		})
		require.NoError(t, err)

		_, err = ts.Service.DeleteMemo(userCtx, &v1pb.DeleteMemoRequest{Name: "memos/owl"})
		require.NoError(t, err)

		// The deletion signal drives the cleanup once the runner catches up.
		ts.Service.SearchService().RunOnce(ctx)
		chunks, err = ts.Store.ListAIIndexChunks(ctx, &store.FindAIIndexChunk{MemoID: &memo.ID})
		require.NoError(t, err)
		require.Empty(t, chunks)
	})
}
