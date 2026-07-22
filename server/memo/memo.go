// Package memo provides the minimal authorized Memo read seam shared by AI features.
package memo

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/usememos/memos/store"
)

// Service is the authorized read seam over memos. It mirrors the read-access
// semantics of the Memo API handlers without depending on the router package.
type Service struct {
	store *store.Store
}

// NewService creates a Service wrapping the given store.
func NewService(s *store.Store) *Service {
	return &Service{store: s}
}

// ReadMemo returns the memo with the given UID if the user may read it.
// A nil user represents an anonymous caller.
func (s *Service) ReadMemo(ctx context.Context, user *store.User, uid string) (*store.Memo, error) {
	memo, err := s.store.GetMemo(ctx, &store.FindMemo{UID: &uid})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get memo")
	}
	if memo == nil {
		return nil, status.Errorf(codes.NotFound, "memo not found")
	}

	// Archived memos are only visible to their creator.
	if memo.RowStatus == store.Archived && (user == nil || memo.CreatorID != user.ID) {
		return nil, status.Errorf(codes.NotFound, "memo not found")
	}

	if memo.Visibility != store.Public {
		if user == nil {
			return nil, status.Errorf(codes.Unauthenticated, "user not authenticated")
		}
		if memo.Visibility == store.Private && memo.CreatorID != user.ID {
			return nil, status.Errorf(codes.PermissionDenied, "permission denied")
		}
	}
	return memo, nil
}

// ListReadableMemos enumerates the user's own NORMAL, top-level memos.
// A nil user represents an anonymous caller, who may not enumerate memos.
//
// TODO(issue 02): grow this into full searchable-source enumeration shared
// with the MemoService handlers instead of only the caller's own memos.
func (s *Service) ListReadableMemos(ctx context.Context, user *store.User) ([]*store.Memo, error) {
	if user == nil {
		return nil, status.Errorf(codes.Unauthenticated, "user not authenticated")
	}
	normal := store.Normal
	memos, err := s.store.ListMemos(ctx, &store.FindMemo{
		CreatorID:       &user.ID,
		RowStatus:       &normal,
		ExcludeComments: true,
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list memos")
	}
	return memos, nil
}
