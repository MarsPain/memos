// Package memo provides the shared Memo read seam: read authorization,
// visibility and archived-state rules, and searchable-source enumeration
// consumed by both the API v1 Memo handlers and AI features.
package memo

import (
	"context"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/usememos/memos/store"
)

// Service is the authorized read seam over memos. Both the API v1 Memo
// handlers and AI features enforce Memo read policy through it; the store
// stays raw persistence with no authorization behavior of its own.
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
	m, err := s.store.GetMemo(ctx, &store.FindMemo{UID: &uid})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get memo")
	}
	if err := s.CheckReadAccess(user, m); err != nil {
		return nil, err
	}
	return m, nil
}

// CheckReadAccess enforces the single-memo read rules: a missing or archived
// memo is reported as not found unless the caller is its creator, non-public
// memos require authentication, and private memos require the creator.
// A nil user represents an anonymous caller.
func (*Service) CheckReadAccess(user *store.User, m *store.Memo) error {
	if m == nil {
		return status.Errorf(codes.NotFound, "memo not found")
	}

	// Archived memos are only visible to their creator.
	if m.RowStatus == store.Archived && (user == nil || m.CreatorID != user.ID) {
		return status.Errorf(codes.NotFound, "memo not found")
	}

	if m.Visibility != store.Public {
		if user == nil {
			return status.Errorf(codes.Unauthenticated, "user not authenticated")
		}
		if m.Visibility == store.Private && m.CreatorID != user.ID {
			return status.Errorf(codes.PermissionDenied, "permission denied")
		}
	}
	return nil
}

// CheckRelatedReadAccess enforces the read rules for resources attached to a
// memo, such as its attachments and reactions: public memos are open,
// non-public memos require authentication, and private memos require the
// creator or an admin. Archived state does not restrict attached resources.
// A nil user represents an anonymous caller.
func (*Service) CheckRelatedReadAccess(user *store.User, m *store.Memo) error {
	if m == nil {
		return status.Errorf(codes.NotFound, "memo not found")
	}
	if m.Visibility != store.Public {
		if user == nil {
			return status.Errorf(codes.Unauthenticated, "user not authenticated")
		}
		if m.Visibility == store.Private && m.CreatorID != user.ID && user.Role != store.RoleAdmin {
			return status.Errorf(codes.PermissionDenied, "permission denied")
		}
	}
	return nil
}

// ApplyReadScope restricts a memo list query to what the user may read. When
// archived is true, the list holds the caller's own archived memos and false
// is returned for anonymous callers, who may not list archived memos at all.
// Otherwise the list holds NORMAL memos: anonymous callers see PUBLIC memos,
// while authenticated callers see their own memos plus PUBLIC and PROTECTED
// ones, narrowed to PUBLIC and PROTECTED when the query already pins another
// creator. A nil user represents an anonymous caller.
func (*Service) ApplyReadScope(find *store.FindMemo, user *store.User, archived bool) bool {
	if archived {
		rowStatus := store.Archived
		find.RowStatus = &rowStatus
		// Archived memos are only visible to their creator.
		if user == nil {
			return false
		}
		find.CreatorID = &user.ID
		return true
	}

	rowStatus := store.Normal
	find.RowStatus = &rowStatus
	if user == nil {
		find.VisibilityList = []store.Visibility{store.Public}
	} else if find.CreatorID == nil {
		find.Filters = append(find.Filters, readableMemoFilter(user))
	} else if *find.CreatorID != user.ID {
		find.VisibilityList = []store.Visibility{store.Public, store.Protected}
	}
	return true
}

// ReadableMemoFilter returns the CEL filter selecting the memos a user may
// read when memos are enumerated through a relation, such as comments or
// linked memos. A nil user represents an anonymous caller.
func (*Service) ReadableMemoFilter(user *store.User) string {
	return readableMemoFilter(user)
}

// readableMemoFilter builds the CEL visibility filter for the user.
func readableMemoFilter(user *store.User) string {
	if user == nil {
		return `visibility == "PUBLIC"`
	}
	return fmt.Sprintf(`creator_id == %d || visibility in ["PUBLIC", "PROTECTED"]`, user.ID)
}

// ListReadableMemos enumerates the NORMAL, top-level memos the user may read:
// their own memos plus PUBLIC and PROTECTED memos from any creator. It is the
// searchable-source enumeration for AI retrieval, scoped by the same read
// rules the Memo list handlers apply.
// A nil user represents an anonymous caller, who may not enumerate memos.
func (s *Service) ListReadableMemos(ctx context.Context, user *store.User) ([]*store.Memo, error) {
	if user == nil {
		return nil, status.Errorf(codes.Unauthenticated, "user not authenticated")
	}
	find := &store.FindMemo{
		ExcludeComments: true,
	}
	s.ApplyReadScope(find, user, false)
	memos, err := s.store.ListMemos(ctx, find)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list memos")
	}
	return memos, nil
}
