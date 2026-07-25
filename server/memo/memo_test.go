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

	t.Run("returns all readable normal top-level memos", func(t *testing.T) {
		fixture := newMemoTestFixture(ctx, t)
		publicMemo := fixture.createMemo(ctx, t, fixture.owner.ID, "owner-public", store.Public)
		privateMemo := fixture.createMemo(ctx, t, fixture.owner.ID, "owner-private", store.Private)
		otherPublic := fixture.createMemo(ctx, t, fixture.other.ID, "other-public", store.Public)
		otherProtected := fixture.createMemo(ctx, t, fixture.other.ID, "other-protected", store.Protected)
		fixture.createMemo(ctx, t, fixture.other.ID, "other-private", store.Private)

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
		require.True(t, ids[otherPublic.ID])
		require.True(t, ids[otherProtected.ID])
		require.False(t, ids[archivedMemo.ID])
		require.False(t, ids[comment.ID])
		require.Len(t, memos, 4)
	})

	t.Run("hides private memos of others", func(t *testing.T) {
		fixture := newMemoTestFixture(ctx, t)
		fixture.createMemo(ctx, t, fixture.owner.ID, "owner-private", store.Private)
		otherPublic := fixture.createMemo(ctx, t, fixture.other.ID, "other-public", store.Public)

		memos, err := fixture.service.ListReadableMemos(ctx, fixture.other)
		require.NoError(t, err)
		require.Len(t, memos, 1)
		require.Equal(t, otherPublic.ID, memos[0].ID)
	})
}

func TestListSearchableMemos(t *testing.T) {
	ctx := context.Background()

	t.Run("enumerates the corpus in ascending id order", func(t *testing.T) {
		fixture := newMemoTestFixture(ctx, t)
		first := fixture.createMemo(ctx, t, fixture.owner.ID, "first", store.Private)
		second := fixture.createMemo(ctx, t, fixture.other.ID, "second", store.Private)
		third := fixture.createMemo(ctx, t, fixture.owner.ID, "third", store.Public)

		memos, err := fixture.service.ListSearchableMemos(ctx, 0, 100)
		require.NoError(t, err)
		require.Len(t, memos, 3)
		// The corpus includes PRIVATE memos of every creator; authorization
		// happens at retrieval time.
		require.Equal(t, first.ID, memos[0].ID)
		require.Equal(t, second.ID, memos[1].ID)
		require.Equal(t, third.ID, memos[2].ID)
	})

	t.Run("paginates by keyset", func(t *testing.T) {
		fixture := newMemoTestFixture(ctx, t)
		first := fixture.createMemo(ctx, t, fixture.owner.ID, "first", store.Public)
		second := fixture.createMemo(ctx, t, fixture.owner.ID, "second", store.Public)
		third := fixture.createMemo(ctx, t, fixture.owner.ID, "third", store.Public)

		page, err := fixture.service.ListSearchableMemos(ctx, 0, 2)
		require.NoError(t, err)
		require.Len(t, page, 2)
		require.Equal(t, first.ID, page[0].ID)
		require.Equal(t, second.ID, page[1].ID)

		rest, err := fixture.service.ListSearchableMemos(ctx, page[1].ID, 2)
		require.NoError(t, err)
		require.Len(t, rest, 1)
		require.Equal(t, third.ID, rest[0].ID)
	})

	t.Run("excludes archived memos and comments", func(t *testing.T) {
		fixture := newMemoTestFixture(ctx, t)
		parent := fixture.createMemo(ctx, t, fixture.owner.ID, "parent", store.Public)
		archivedMemo := fixture.createMemo(ctx, t, fixture.owner.ID, "archived", store.Public)
		archived := store.Archived
		require.NoError(t, fixture.store.UpdateMemo(ctx, &store.UpdateMemo{ID: archivedMemo.ID, RowStatus: &archived}))
		comment := fixture.createMemo(ctx, t, fixture.owner.ID, "comment", store.Public)
		_, err := fixture.store.UpsertMemoRelation(ctx, &store.MemoRelation{
			MemoID:        comment.ID,
			RelatedMemoID: parent.ID,
			Type:          store.MemoRelationComment,
		})
		require.NoError(t, err)

		memos, err := fixture.service.ListSearchableMemos(ctx, 0, 100)
		require.NoError(t, err)
		require.Len(t, memos, 1)
		require.Equal(t, parent.ID, memos[0].ID)
	})
}

func TestGetSearchableMemo(t *testing.T) {
	ctx := context.Background()
	fixture := newMemoTestFixture(ctx, t)

	normal := fixture.createMemo(ctx, t, fixture.owner.ID, "normal", store.Private)
	archivedMemo := fixture.createMemo(ctx, t, fixture.owner.ID, "archived", store.Public)
	archived := store.Archived
	require.NoError(t, fixture.store.UpdateMemo(ctx, &store.UpdateMemo{ID: archivedMemo.ID, RowStatus: &archived}))
	comment := fixture.createMemo(ctx, t, fixture.owner.ID, "comment", store.Public)
	_, err := fixture.store.UpsertMemoRelation(ctx, &store.MemoRelation{
		MemoID:        comment.ID,
		RelatedMemoID: normal.ID,
		Type:          store.MemoRelationComment,
	})
	require.NoError(t, err)

	t.Run("returns corpus memos of any visibility", func(t *testing.T) {
		m, err := fixture.service.GetSearchableMemo(ctx, normal.ID)
		require.NoError(t, err)
		require.NotNil(t, m)
		require.Equal(t, normal.ID, m.ID)
	})

	t.Run("archived memos are out of the corpus", func(t *testing.T) {
		m, err := fixture.service.GetSearchableMemo(ctx, archivedMemo.ID)
		require.NoError(t, err)
		require.Nil(t, m)
	})

	t.Run("comments are out of the corpus", func(t *testing.T) {
		m, err := fixture.service.GetSearchableMemo(ctx, comment.ID)
		require.NoError(t, err)
		require.Nil(t, m)
	})

	t.Run("missing memos return nil", func(t *testing.T) {
		m, err := fixture.service.GetSearchableMemo(ctx, 999)
		require.NoError(t, err)
		require.Nil(t, m)
	})
}

func TestCheckReadAccess(t *testing.T) {
	service := memo.NewService(nil)
	owner := &store.User{ID: 1, Role: store.RoleUser}
	other := &store.User{ID: 2, Role: store.RoleUser}
	admin := &store.User{ID: 3, Role: store.RoleAdmin}

	t.Run("missing memo is not found", func(t *testing.T) {
		require.Equal(t, codes.NotFound, status.Code(service.CheckReadAccess(owner, nil)))
	})

	t.Run("public memo is open to anyone", func(t *testing.T) {
		m := &store.Memo{CreatorID: owner.ID, Visibility: store.Public}
		require.NoError(t, service.CheckReadAccess(nil, m))
		require.NoError(t, service.CheckReadAccess(other, m))
	})

	t.Run("archived memo is hidden from everyone but its creator", func(t *testing.T) {
		m := &store.Memo{CreatorID: owner.ID, RowStatus: store.Archived, Visibility: store.Public}
		require.Equal(t, codes.NotFound, status.Code(service.CheckReadAccess(nil, m)))
		require.Equal(t, codes.NotFound, status.Code(service.CheckReadAccess(other, m)))
		require.NoError(t, service.CheckReadAccess(owner, m))
	})

	t.Run("non-public memos require authentication", func(t *testing.T) {
		protected := &store.Memo{CreatorID: owner.ID, Visibility: store.Protected}
		require.Equal(t, codes.Unauthenticated, status.Code(service.CheckReadAccess(nil, protected)))
		private := &store.Memo{CreatorID: owner.ID, Visibility: store.Private}
		require.Equal(t, codes.Unauthenticated, status.Code(service.CheckReadAccess(nil, private)))
	})

	t.Run("protected memo is open to any authenticated user", func(t *testing.T) {
		m := &store.Memo{CreatorID: owner.ID, Visibility: store.Protected}
		require.NoError(t, service.CheckReadAccess(other, m))
	})

	t.Run("private memo requires the creator", func(t *testing.T) {
		m := &store.Memo{CreatorID: owner.ID, Visibility: store.Private}
		require.Equal(t, codes.PermissionDenied, status.Code(service.CheckReadAccess(other, m)))
		// Admins get no bypass on the memo read path.
		require.Equal(t, codes.PermissionDenied, status.Code(service.CheckReadAccess(admin, m)))
		require.NoError(t, service.CheckReadAccess(owner, m))
	})
}

func TestCheckRelatedReadAccess(t *testing.T) {
	service := memo.NewService(nil)
	owner := &store.User{ID: 1, Role: store.RoleUser}
	other := &store.User{ID: 2, Role: store.RoleUser}
	admin := &store.User{ID: 3, Role: store.RoleAdmin}

	t.Run("missing memo is not found", func(t *testing.T) {
		require.Equal(t, codes.NotFound, status.Code(service.CheckRelatedReadAccess(owner, nil)))
	})

	t.Run("public memo is open to anyone", func(t *testing.T) {
		m := &store.Memo{CreatorID: owner.ID, Visibility: store.Public}
		require.NoError(t, service.CheckRelatedReadAccess(nil, m))
		require.NoError(t, service.CheckRelatedReadAccess(other, m))
	})

	t.Run("non-public memos require authentication", func(t *testing.T) {
		protected := &store.Memo{CreatorID: owner.ID, Visibility: store.Protected}
		require.Equal(t, codes.Unauthenticated, status.Code(service.CheckRelatedReadAccess(nil, protected)))
		private := &store.Memo{CreatorID: owner.ID, Visibility: store.Private}
		require.Equal(t, codes.Unauthenticated, status.Code(service.CheckRelatedReadAccess(nil, private)))
	})

	t.Run("protected memo is open to any authenticated user", func(t *testing.T) {
		m := &store.Memo{CreatorID: owner.ID, Visibility: store.Protected}
		require.NoError(t, service.CheckRelatedReadAccess(other, m))
	})

	t.Run("private memo requires the creator or an admin", func(t *testing.T) {
		m := &store.Memo{CreatorID: owner.ID, Visibility: store.Private}
		require.Equal(t, codes.PermissionDenied, status.Code(service.CheckRelatedReadAccess(other, m)))
		require.NoError(t, service.CheckRelatedReadAccess(admin, m))
		require.NoError(t, service.CheckRelatedReadAccess(owner, m))
	})

	t.Run("archived state does not restrict attached resources", func(t *testing.T) {
		m := &store.Memo{CreatorID: owner.ID, RowStatus: store.Archived, Visibility: store.Public}
		require.NoError(t, service.CheckRelatedReadAccess(nil, m))
		require.NoError(t, service.CheckRelatedReadAccess(other, m))
	})
}

func TestApplyReadScope(t *testing.T) {
	service := memo.NewService(nil)
	user := &store.User{ID: 7, Role: store.RoleUser}

	t.Run("archived list for anonymous is empty", func(t *testing.T) {
		find := &store.FindMemo{}
		require.False(t, service.ApplyReadScope(find, nil, true))
		require.Equal(t, store.Archived, *find.RowStatus)
		require.Nil(t, find.CreatorID)
	})

	t.Run("archived list is restricted to the creator", func(t *testing.T) {
		find := &store.FindMemo{}
		require.True(t, service.ApplyReadScope(find, user, true))
		require.Equal(t, store.Archived, *find.RowStatus)
		require.Equal(t, user.ID, *find.CreatorID)
	})

	t.Run("anonymous lists public normal memos", func(t *testing.T) {
		find := &store.FindMemo{}
		require.True(t, service.ApplyReadScope(find, nil, false))
		require.Equal(t, store.Normal, *find.RowStatus)
		require.Equal(t, []store.Visibility{store.Public}, find.VisibilityList)
		require.Empty(t, find.Filters)
	})

	t.Run("authenticated list adds the readability filter", func(t *testing.T) {
		find := &store.FindMemo{}
		require.True(t, service.ApplyReadScope(find, user, false))
		require.Equal(t, store.Normal, *find.RowStatus)
		require.Empty(t, find.VisibilityList)
		require.Equal(t, []string{`creator_id == 7 || visibility in ["PUBLIC", "PROTECTED"]`}, find.Filters)
	})

	t.Run("readability filter is appended after existing filters", func(t *testing.T) {
		find := &store.FindMemo{Filters: []string{`pinned == true`}}
		require.True(t, service.ApplyReadScope(find, user, false))
		require.Equal(t, []string{`pinned == true`, `creator_id == 7 || visibility in ["PUBLIC", "PROTECTED"]`}, find.Filters)
	})

	t.Run("listing another creator narrows to public and protected", func(t *testing.T) {
		otherID := int32(8)
		find := &store.FindMemo{CreatorID: &otherID}
		require.True(t, service.ApplyReadScope(find, user, false))
		require.Equal(t, []store.Visibility{store.Public, store.Protected}, find.VisibilityList)
		require.Empty(t, find.Filters)
	})

	t.Run("listing own memos adds no restriction", func(t *testing.T) {
		ownID := user.ID
		find := &store.FindMemo{CreatorID: &ownID}
		require.True(t, service.ApplyReadScope(find, user, false))
		require.Empty(t, find.VisibilityList)
		require.Empty(t, find.Filters)
	})
}

func TestReadableMemoFilter(t *testing.T) {
	service := memo.NewService(nil)
	require.Equal(t, `visibility == "PUBLIC"`, service.ReadableMemoFilter(nil))
	require.Equal(t, `creator_id == 7 || visibility in ["PUBLIC", "PROTECTED"]`, service.ReadableMemoFilter(&store.User{ID: 7}))
}
