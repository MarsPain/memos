package search

import (
	"context"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"

	"github.com/usememos/memos/internal/markdown"
	"github.com/usememos/memos/server/memo"
	"github.com/usememos/memos/store"
	teststore "github.com/usememos/memos/store/test"
)

type searchTestFixture struct {
	service *Service
	store   *store.Store
	owner   *store.User
}

func newSearchTestFixture(ctx context.Context, t testing.TB) *searchTestFixture {
	t.Helper()
	st := teststore.NewTestingStore(ctx, t)
	t.Cleanup(func() { _ = st.Close() })
	owner, err := st.CreateUser(ctx, &store.User{Username: "owner", Role: store.RoleUser, Email: "owner@example.com"})
	require.NoError(t, err)
	markdownService := markdown.NewService(markdown.WithTagExtension())
	return &searchTestFixture{
		service: NewService(st, memo.NewService(st), markdownService),
		store:   st,
		owner:   owner,
	}
}

func (f *searchTestFixture) createMemo(ctx context.Context, t testing.TB, uid, content string) *store.Memo {
	t.Helper()
	created, err := f.store.CreateMemo(ctx, &store.Memo{
		UID:        uid,
		CreatorID:  f.owner.ID,
		Content:    content,
		Visibility: store.Private,
	})
	require.NoError(t, err)
	return created
}

func (f *searchTestFixture) getDocument(ctx context.Context, t testing.TB, memoID int32) *store.AISearchDocument {
	t.Helper()
	document, err := f.store.GetAISearchDocument(ctx, &store.FindAISearchDocument{MemoID: &memoID})
	require.NoError(t, err)
	return document
}

func TestRunOnceBuildsCorpusDocuments(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)

	normal := fixture.createMemo(ctx, t, "normal", "# Field Notes\n\nThe owl hunts at dusk. #birds")
	archivedMemo := fixture.createMemo(ctx, t, "archived", "archived content")
	archived := store.Archived
	require.NoError(t, fixture.store.UpdateMemo(ctx, &store.UpdateMemo{ID: archivedMemo.ID, RowStatus: &archived}))
	comment := fixture.createMemo(ctx, t, "comment", "a comment")
	_, err := fixture.store.UpsertMemoRelation(ctx, &store.MemoRelation{
		MemoID:        comment.ID,
		RelatedMemoID: normal.ID,
		Type:          store.MemoRelationComment,
	})
	require.NoError(t, err)

	fixture.service.RunOnce(ctx)

	document := fixture.getDocument(ctx, t, normal.ID)
	require.NotNil(t, document)
	require.Equal(t, "field notes", document.Title)
	require.Equal(t, []string{"birds"}, document.Tags)
	require.Equal(t, "field notes the owl hunts at dusk. #birds", document.Content)
	require.Equal(t, contentHash(normal.Content), document.ContentHash)
	require.Equal(t, ProjectionVersion, document.ProjectionVersion)
	require.Equal(t, NormalizationVersion, document.NormalizationVersion)
	require.Equal(t, normal.UpdatedTs, document.MemoUpdatedTs)
	require.Equal(t, normal.UID, document.MemoUID)

	// Archived memos and comments are out of the corpus.
	require.Nil(t, fixture.getDocument(ctx, t, archivedMemo.ID))
	require.Nil(t, fixture.getDocument(ctx, t, comment.ID))
}

func TestRunOnceTracksUpdatesArchivesAndDeletes(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)

	m := fixture.createMemo(ctx, t, "tracked", "first version")
	fixture.service.RunOnce(ctx)
	document := fixture.getDocument(ctx, t, m.ID)
	require.NotNil(t, document)
	require.Equal(t, "first version", document.Content)

	// A content update is reflected after reconciliation.
	newContent := "second version"
	require.NoError(t, fixture.store.UpdateMemo(ctx, &store.UpdateMemo{ID: m.ID, Content: &newContent}))
	fixture.service.RunOnce(ctx)
	document = fixture.getDocument(ctx, t, m.ID)
	require.Equal(t, "second version", document.Content)
	require.Equal(t, contentHash(newContent), document.ContentHash)

	// Archiving removes the document.
	archived := store.Archived
	require.NoError(t, fixture.store.UpdateMemo(ctx, &store.UpdateMemo{ID: m.ID, RowStatus: &archived}))
	fixture.service.RunOnce(ctx)
	require.Nil(t, fixture.getDocument(ctx, t, m.ID))

	// Unarchiving brings it back.
	normal := store.Normal
	require.NoError(t, fixture.store.UpdateMemo(ctx, &store.UpdateMemo{ID: m.ID, RowStatus: &normal}))
	fixture.service.RunOnce(ctx)
	require.NotNil(t, fixture.getDocument(ctx, t, m.ID))

	// Deleting the memo removes the document without touching the memo path.
	require.NoError(t, fixture.store.DeleteMemo(ctx, &store.DeleteMemo{ID: m.ID}))
	fixture.service.RunOnce(ctx)
	require.Nil(t, fixture.getDocument(ctx, t, m.ID))
}

func TestInvalidationSignalsSyncOutsideTheWritePath(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)

	m := fixture.createMemo(ctx, t, "signaled", "signaled content")
	// Nothing is built until the signal is processed.
	require.Nil(t, fixture.getDocument(ctx, t, m.ID))

	fixture.service.Invalidate(m.ID)
	fixture.service.syncDirty(ctx)
	require.NotNil(t, fixture.getDocument(ctx, t, m.ID))

	// A deletion signal removes the document.
	require.NoError(t, fixture.store.DeleteMemo(ctx, &store.DeleteMemo{ID: m.ID}))
	fixture.service.Invalidate(m.ID)
	fixture.service.syncDirty(ctx)
	require.Nil(t, fixture.getDocument(ctx, t, m.ID))
}

func TestRunOnceRebuildsStaleVersionsAndDroppedDocuments(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)

	m := fixture.createMemo(ctx, t, "rebuilt", "rebuild me")
	fixture.service.RunOnce(ctx)
	require.NotNil(t, fixture.getDocument(ctx, t, m.ID))

	// A document from another projection version is rebuilt.
	_, err := fixture.store.UpsertAISearchDocument(ctx, &store.AISearchDocument{
		MemoID:               m.ID,
		MemoUID:              m.UID,
		MemoUpdatedTs:        m.UpdatedTs,
		ContentHash:          "stale-hash",
		ProjectionVersion:    ProjectionVersion - 1,
		NormalizationVersion: NormalizationVersion,
		Content:              "stale content",
	})
	require.NoError(t, err)
	fixture.service.RunOnce(ctx)
	document := fixture.getDocument(ctx, t, m.ID)
	require.Equal(t, ProjectionVersion, document.ProjectionVersion)
	require.Equal(t, "rebuild me", document.Content)
	require.Equal(t, contentHash(m.Content), document.ContentHash)

	// A dropped document is rebuilt from source without touching the memo.
	require.NoError(t, fixture.store.DeleteAISearchDocument(ctx, &store.DeleteAISearchDocument{MemoID: m.ID}))
	require.Nil(t, fixture.getDocument(ctx, t, m.ID))
	fixture.service.RunOnce(ctx)
	document = fixture.getDocument(ctx, t, m.ID)
	require.NotNil(t, document)
	require.Equal(t, "rebuild me", document.Content)
}

func TestPerItemBackoff(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)

	m := fixture.createMemo(ctx, t, "backoff", "backoff content")

	// A memo in backoff is skipped even when its content changed.
	fixture.service.recordResult(m.ID, errors.New("boom"))
	require.False(t, fixture.service.retryAllowed(m.ID))
	fixture.service.syncMemo(ctx, m)
	require.Nil(t, fixture.getDocument(ctx, t, m.ID))

	// Once the backoff expires the sync runs again.
	fixture.service.backoff.failures[m.ID].nextRetry = time.Now().Add(-time.Second)
	require.True(t, fixture.service.retryAllowed(m.ID))
	fixture.service.syncMemo(ctx, m)
	require.NotNil(t, fixture.getDocument(ctx, t, m.ID))

	// Success cleared the backoff record.
	fixture.service.mu.Lock()
	_, failed := fixture.service.backoff.failures[m.ID]
	fixture.service.mu.Unlock()
	require.False(t, failed)
}
