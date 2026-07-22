package memo_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/usememos/memos/server/memo"
	"github.com/usememos/memos/store"
	teststore "github.com/usememos/memos/store/test"
)

type memoTestFixture struct {
	service *memo.Service
	store   *store.Store
	owner   *store.User
	other   *store.User
}

func newMemoTestFixture(ctx context.Context, t *testing.T) *memoTestFixture {
	st := teststore.NewTestingStore(ctx, t)
	t.Cleanup(func() { _ = st.Close() })
	owner, err := st.CreateUser(ctx, &store.User{Username: "owner", Role: store.RoleUser, Email: "owner@example.com"})
	require.NoError(t, err)
	other, err := st.CreateUser(ctx, &store.User{Username: "other", Role: store.RoleUser, Email: "other@example.com"})
	require.NoError(t, err)
	return &memoTestFixture{
		service: memo.NewService(st),
		store:   st,
		owner:   owner,
		other:   other,
	}
}

func (f *memoTestFixture) createMemo(ctx context.Context, t *testing.T, creatorID int32, uid string, visibility store.Visibility) *store.Memo {
	t.Helper()
	created, err := f.store.CreateMemo(ctx, &store.Memo{
		UID:        uid,
		CreatorID:  creatorID,
		Content:    "content of " + uid,
		Visibility: visibility,
	})
	require.NoError(t, err)
	return created
}

func TestReadMemo(t *testing.T) {
	ctx := context.Background()

	t.Run("missing memo is not found", func(t *testing.T) {
		fixture := newMemoTestFixture(ctx, t)
		_, err := fixture.service.ReadMemo(ctx, fixture.owner, "missing")
		require.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("public memo readable by anonymous", func(t *testing.T) {
		fixture := newMemoTestFixture(ctx, t)
		created := fixture.createMemo(ctx, t, fixture.owner.ID, "public-memo", store.Public)
		found, err := fixture.service.ReadMemo(ctx, nil, created.UID)
		require.NoError(t, err)
		require.Equal(t, created.ID, found.ID)
	})

	t.Run("private memo readable by creator", func(t *testing.T) {
		fixture := newMemoTestFixture(ctx, t)
		created := fixture.createMemo(ctx, t, fixture.owner.ID, "private-memo", store.Private)
		found, err := fixture.service.ReadMemo(ctx, fixture.owner, created.UID)
		require.NoError(t, err)
		require.Equal(t, created.ID, found.ID)
	})

	t.Run("private memo denied for other user", func(t *testing.T) {
		fixture := newMemoTestFixture(ctx, t)
		created := fixture.createMemo(ctx, t, fixture.owner.ID, "private-memo", store.Private)
		_, err := fixture.service.ReadMemo(ctx, fixture.other, created.UID)
		require.Equal(t, codes.PermissionDenied, status.Code(err))
	})

	t.Run("private memo requires authentication", func(t *testing.T) {
		fixture := newMemoTestFixture(ctx, t)
		created := fixture.createMemo(ctx, t, fixture.owner.ID, "private-memo", store.Private)
		_, err := fixture.service.ReadMemo(ctx, nil, created.UID)
		require.Equal(t, codes.Unauthenticated, status.Code(err))
	})

	t.Run("protected memo requires authentication", func(t *testing.T) {
		fixture := newMemoTestFixture(ctx, t)
		created := fixture.createMemo(ctx, t, fixture.owner.ID, "protected-memo", store.Protected)
		_, err := fixture.service.ReadMemo(ctx, nil, created.UID)
		require.Equal(t, codes.Unauthenticated, status.Code(err))
	})

	t.Run("protected memo readable by any authenticated user", func(t *testing.T) {
		fixture := newMemoTestFixture(ctx, t)
		created := fixture.createMemo(ctx, t, fixture.owner.ID, "protected-memo", store.Protected)
		found, err := fixture.service.ReadMemo(ctx, fixture.other, created.UID)
		require.NoError(t, err)
		require.Equal(t, created.ID, found.ID)
	})

	t.Run("archived memo hidden from non-creator", func(t *testing.T) {
		fixture := newMemoTestFixture(ctx, t)
		created := fixture.createMemo(ctx, t, fixture.owner.ID, "archived-memo", store.Public)
		archived := store.Archived
		require.NoError(t, fixture.store.UpdateMemo(ctx, &store.UpdateMemo{ID: created.ID, RowStatus: &archived}))
		_, err := fixture.service.ReadMemo(ctx, fixture.other, created.UID)
		require.Equal(t, codes.NotFound, status.Code(err))
		_, err = fixture.service.ReadMemo(ctx, nil, created.UID)
		require.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("archived memo readable by creator", func(t *testing.T) {
		fixture := newMemoTestFixture(ctx, t)
		created := fixture.createMemo(ctx, t, fixture.owner.ID, "archived-memo", store.Public)
		archived := store.Archived
		require.NoError(t, fixture.store.UpdateMemo(ctx, &store.UpdateMemo{ID: created.ID, RowStatus: &archived}))
		found, err := fixture.service.ReadMemo(ctx, fixture.owner, created.UID)
		require.NoError(t, err)
		require.Equal(t, created.ID, found.ID)
	})
}

func TestListReadableMemos(t *testing.T) {
	ctx := context.Background()

	t.Run("anonymous is unauthenticated", func(t *testing.T) {
		fixture := newMemoTestFixture(ctx, t)
		_, err := fixture.service.ListReadableMemos(ctx, nil)
		require.Equal(t, codes.Unauthenticated, status.Code(err))
	})

	t.Run("returns only own normal top-level memos", func(t *testing.T) {
		fixture := newMemoTestFixture(ctx, t)
		publicMemo := fixture.createMemo(ctx, t, fixture.owner.ID, "owner-public", store.Public)
		privateMemo := fixture.createMemo(ctx, t, fixture.owner.ID, "owner-private", store.Private)
		fixture.createMemo(ctx, t, fixture.other.ID, "other-public", store.Public)

		archivedMemo := fixture.createMemo(ctx, t, fixture.owner.ID, "owner-archived", store.Public)
		archived := store.Archived
		require.NoError(t, fixture.store.UpdateMemo(ctx, &store.UpdateMemo{ID: archivedMemo.ID, RowStatus: &archived}))

		comment := fixture.createMemo(ctx, t, fixture.owner.ID, "owner-comment", store.Public)
		_, err := fixture.store.UpsertMemoRelation(ctx, &store.MemoRelation{
			MemoID:        comment.ID,
			RelatedMemoID: publicMemo.ID,
			Type:          store.MemoRelationComment,
		})
		require.NoError(t, err)

		memos, err := fixture.service.ListReadableMemos(ctx, fixture.owner)
		require.NoError(t, err)
		ids := make(map[int32]bool, len(memos))
		for _, m := range memos {
			ids[m.ID] = true
			require.Equal(t, store.Normal, m.RowStatus)
			require.Nil(t, m.ParentUID)
		}
		require.True(t, ids[publicMemo.ID])
		require.True(t, ids[privateMemo.ID])
		require.False(t, ids[archivedMemo.ID])
		require.False(t, ids[comment.ID])
		require.Len(t, memos, 2)
	})
}
