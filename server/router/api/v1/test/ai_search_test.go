package test

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	v1pb "github.com/usememos/memos/proto/gen/api/v1"
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
