package v1

import (
	"context"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/usememos/memos/internal/ai/gateway"
	"github.com/usememos/memos/internal/markdown"
	v1pb "github.com/usememos/memos/proto/gen/api/v1"
	serversearch "github.com/usememos/memos/server/ai/search"
)

// SearchMemos searches the memos the caller may read with exact, partial,
// and typo-tolerant matching over the derived search documents. Every result
// is reauthorized and revision-checked against the current source memo
// before its snippet is returned.
func (s *APIV1Service) SearchMemos(ctx context.Context, request *v1pb.SearchMemosRequest) (*v1pb.SearchMemosResponse, error) {
	user, err := s.fetchCurrentUser(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get current user: %v", err)
	}
	if user == nil {
		return nil, status.Errorf(codes.Unauthenticated, "user not authenticated")
	}

	filters, err := convertSearchMemosFilter(request.GetFilter())
	if err != nil {
		return nil, err
	}
	outcome, err := s.retriever().Search(ctx, user, serversearch.Query{
		Text:    request.GetQuery(),
		Filters: filters,
	})
	if err != nil {
		return nil, err
	}

	response := &v1pb.SearchMemosResponse{
		Results:        make([]*v1pb.MemoSearchResult, 0, len(outcome.Hits)),
		PartialReasons: outcome.PartialReasons,
	}
	for _, hit := range outcome.Hits {
		response.Results = append(response.Results, &v1pb.MemoSearchResult{
			Memo:           MemoNamePrefix + hit.MemoUID,
			Snippet:        hit.Snippet,
			RankReasons:    hit.RankReasons,
			SourceRevision: timestamppb.New(time.Unix(hit.SourceRevision, 0)),
			SourceHash:     hit.SourceHash,
			SourceStart:    int32(hit.SourceStart),
			SourceEnd:      int32(hit.SourceEnd),
		})
	}
	return response, nil
}

// convertSearchMemosFilter translates the API filter into retrieval filters,
// resolving the creator resource name to a user ID.
func convertSearchMemosFilter(filter *v1pb.SearchMemosFilter) (serversearch.Filters, error) {
	filters := serversearch.Filters{Tags: filter.GetTags()}
	if filter.GetVisibility() != v1pb.Visibility_VISIBILITY_UNSPECIFIED {
		visibility := convertVisibilityToStore(filter.GetVisibility())
		filters.Visibility = &visibility
	}
	if filter.GetCreatedAfter() != nil {
		after := filter.GetCreatedAfter().AsTime()
		filters.CreatedAfter = &after
	}
	if filter.GetCreatedBefore() != nil {
		before := filter.GetCreatedBefore().AsTime()
		filters.CreatedBefore = &before
	}
	creator := strings.TrimSpace(filter.GetCreator())
	if creator != "" {
		creatorID, err := ExtractUserIDFromName(creator)
		if err != nil {
			return filters, status.Errorf(codes.InvalidArgument, "invalid creator name: %q", creator)
		}
		filters.CreatorID = &creatorID
	}
	return filters, nil
}

// retriever lazily builds the retrieval service behind the unified search
// operation. Tests construct APIV1Service by struct literal, so the
// retriever is built on first use like the other AI services.
func (s *APIV1Service) retriever() *serversearch.Retriever {
	s.searchRetrieverOnce.Do(func() {
		markdownService := s.MarkdownService
		if markdownService == nil {
			markdownService = defaultMarkdownService()
		}
		s.searchRetriever = serversearch.NewRetriever(s.Store, s.memoReadService(), markdownService, serversearch.DefaultBudgets())
		s.searchRetriever.SetSemanticSearcher(serversearch.NewSemanticSearcher(s.Store, func() gateway.ModelFactory {
			return s.AIModelFactory
		}))
	})
	return s.searchRetriever
}

// defaultMarkdownService builds the markdown service used when the service
// was constructed by struct literal without one.
func defaultMarkdownService() markdown.Service {
	return markdown.NewService(
		markdown.WithTagExtension(),
		markdown.WithMentionExtension(),
	)
}
