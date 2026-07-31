package v1

import (
	"context"
	"net/http"
	"sync"

	"connectrpc.com/connect"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/labstack/echo/v5"
	"golang.org/x/sync/semaphore"

	"github.com/usememos/memos/internal/ai/gateway"
	"github.com/usememos/memos/internal/markdown"
	"github.com/usememos/memos/internal/profile"
	v1pb "github.com/usememos/memos/proto/gen/api/v1"
	serverai "github.com/usememos/memos/server/ai"
	"github.com/usememos/memos/server/ai/search"
	"github.com/usememos/memos/server/auth"
	"github.com/usememos/memos/server/memo"
	"github.com/usememos/memos/server/notification"
	"github.com/usememos/memos/store"
)

const maxAPIRequestBytes = 256 << 20

type APIV1Service struct {
	v1pb.UnimplementedInstanceServiceServer
	v1pb.UnimplementedAuthServiceServer
	v1pb.UnimplementedUserServiceServer
	v1pb.UnimplementedMemoServiceServer
	v1pb.UnimplementedAttachmentServiceServer
	v1pb.UnimplementedAIServiceServer
	v1pb.UnimplementedShortcutServiceServer
	v1pb.UnimplementedIdentityProviderServiceServer

	Secret                  string
	Profile                 *profile.Profile
	Store                   *store.Store
	MarkdownService         markdown.Service
	SSEHub                  *SSEHub
	NotificationEmailSender notification.EmailSender
	AIModelFactory          gateway.ModelFactory

	// thumbnailSemaphore limits concurrent thumbnail generation to prevent memory exhaustion
	thumbnailSemaphore       *semaphore.Weighted
	imageProcessingSemaphore *semaphore.Weighted

	// instanceStatsCache memoizes GetInstanceStats results for instanceStatsCacheTTL.
	instanceStatsCache instanceStatsCache

	// aiChatOnce and aiChat lazily build the chat service so tests that
	// construct APIV1Service by struct literal still get a working service.
	aiChatOnce sync.Once
	aiChat     *serverai.Service

	// memoReadOnce and memoRead lazily build the shared memo read seam for
	// the same struct-literal reason as aiChat.
	memoReadOnce sync.Once
	memoRead     *memo.Service

	// searchOnce and search lazily build the search-document service for the
	// same struct-literal reason as aiChat.
	searchOnce sync.Once
	search     *search.Service

	// searchRetrieverOnce and searchRetriever lazily build the retrieval
	// service for the same struct-literal reason as aiChat.
	searchRetrieverOnce sync.Once
	searchRetriever     *search.Retriever

	// aiIndexerOnce and aiIndexer lazily build the manually triggered
	// embedding indexer (Scaffold B) for the same struct-literal reason as
	// aiChat.
	aiIndexerOnce sync.Once
	aiIndexer     *search.Indexer
}

func NewAPIV1Service(secret string, profile *profile.Profile, store *store.Store) *APIV1Service {
	markdownService := markdown.NewService(
		markdown.WithTagExtension(),
		markdown.WithMentionExtension(),
	)
	return &APIV1Service{
		Secret:                   secret,
		Profile:                  profile,
		Store:                    store,
		MarkdownService:          markdownService,
		SSEHub:                   NewSSEHub(),
		NotificationEmailSender:  nil,
		AIModelFactory:           gateway.NewModel,
		thumbnailSemaphore:       semaphore.NewWeighted(3), // Limit to 3 concurrent thumbnail generations
		imageProcessingSemaphore: semaphore.NewWeighted(2),
	}
}

// chatService lazily builds the AI chat service. Tests construct APIV1Service
// by struct literal and may override AIModelFactory afterwards, so the service
// is built on first use and resolves the factory through an indirection.
func (s *APIV1Service) chatService() *serverai.Service {
	s.aiChatOnce.Do(func() {
		s.aiChat = serverai.NewService(s.Store, s.memoReadService(), func() gateway.ModelFactory {
			return s.AIModelFactory
		})
	})
	return s.aiChat
}

// memoReadService lazily builds the shared memo read seam through which all
// Memo read authorization, visibility and archived-state rules are enforced.
func (s *APIV1Service) memoReadService() *memo.Service {
	s.memoReadOnce.Do(func() {
		s.memoRead = memo.NewService(s.Store)
	})
	return s.memoRead
}

// SearchService lazily builds the search-document service that maintains the
// derived ai_search_document corpus. Memo write handlers emit invalidation
// signals to it; the server runs its reconciliation loop in the background.
func (s *APIV1Service) SearchService() *search.Service {
	s.searchOnce.Do(func() {
		markdownService := s.MarkdownService
		if markdownService == nil {
			markdownService = markdown.NewService(
				markdown.WithTagExtension(),
				markdown.WithMentionExtension(),
			)
		}
		s.search = search.NewService(s.Store, s.memoReadService(), markdownService)
	})
	return s.search
}

// AISearchIndexer lazily builds the manually triggered embedding indexer
// (Scaffold B; TODO(issue 03): runner-integrated reconciliation replaces the
// manual trigger). Tests may override AIModelFactory before first use.
func (s *APIV1Service) AISearchIndexer() *search.Indexer {
	s.aiIndexerOnce.Do(func() {
		s.aiIndexer = search.NewIndexer(s.Store, func() gateway.ModelFactory {
			return s.AIModelFactory
		})
	})
	return s.aiIndexer
}

// RegisterGateway registers the gRPC-Gateway and Connect handlers with the given Echo instance.
func (s *APIV1Service) RegisterGateway(ctx context.Context, echoServer *echo.Echo) error {
	// Shared authorizer: one source of truth for authentication and anonymous-access
	// policy, used by both the gRPC-Gateway middleware and the Connect interceptor.
	authorizer := NewAuthorizer(s.Store, s.Secret, s.Profile)
	gatewayAuthMiddleware := func(next runtime.HandlerFunc) runtime.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request, pathParams map[string]string) {
			ctx := r.Context()

			// The RPC method name is set by grpc-gateway after routing. When it can't be
			// determined, skip the policy check and let the service layer handle visibility.
			rpcMethod, ok := runtime.RPCMethod(ctx)
			authHeader := r.Header.Get("Authorization")

			result := authorizer.Authenticate(ctx, authHeader)
			if ok {
				if err := authorizer.CheckAccess(ctx, rpcMethod, result); err != nil {
					http.Error(w, `{"code": 16, "message": "authentication required"}`, http.StatusUnauthorized)
					return
				}
			}

			// Apply the identity to the context (no-op for permitted anonymous requests).
			if result != nil {
				r = r.WithContext(auth.ApplyToContext(ctx, result))
			}

			next(w, r, pathParams)
		}
	}

	// Create gRPC-Gateway mux with auth middleware.
	gwMux := runtime.NewServeMux(
		runtime.WithMiddlewares(gatewayAuthMiddleware),
	)
	if err := v1pb.RegisterInstanceServiceHandlerServer(ctx, gwMux, s); err != nil {
		return err
	}
	if err := v1pb.RegisterAuthServiceHandlerServer(ctx, gwMux, s); err != nil {
		return err
	}
	if err := v1pb.RegisterUserServiceHandlerServer(ctx, gwMux, s); err != nil {
		return err
	}
	if err := v1pb.RegisterMemoServiceHandlerServer(ctx, gwMux, s); err != nil {
		return err
	}
	if err := v1pb.RegisterAttachmentServiceHandlerServer(ctx, gwMux, s); err != nil {
		return err
	}
	if err := v1pb.RegisterAIServiceHandlerServer(ctx, gwMux, s); err != nil {
		return err
	}
	if err := v1pb.RegisterShortcutServiceHandlerServer(ctx, gwMux, s); err != nil {
		return err
	}
	if err := v1pb.RegisterIdentityProviderServiceHandlerServer(ctx, gwMux, s); err != nil {
		return err
	}
	gwGroup := echoServer.Group("")
	// Register SSE endpoint with same CORS as rest of /api/v1.
	RegisterSSERoutes(gwGroup, s.SSEHub, s.Store, s.Secret)
	handler := echo.WrapHandler(http.MaxBytesHandler(gwMux, maxAPIRequestBytes))

	gwGroup.Any("/api/v1/*", handler)
	gwGroup.Any("/file/*", handler)

	// Connect handlers for browser clients (replaces grpc-web).
	logStacktraces := s.Profile.Demo
	connectInterceptors := connect.WithInterceptors(
		NewMetadataInterceptor(), // Convert HTTP headers to gRPC metadata first
		NewLoggingInterceptor(logStacktraces),
		NewRecoveryInterceptor(logStacktraces),
		NewAuthInterceptor(authorizer),
	)
	connectMux := http.NewServeMux()
	connectHandler := NewConnectServiceHandler(s)
	connectHandler.RegisterConnectHandlers(connectMux, connectInterceptors, connect.WithReadMaxBytes(maxAPIRequestBytes))

	connectGroup := echoServer.Group("")
	connectGroup.Any("/memos.api.v1.*", echo.WrapHandler(http.MaxBytesHandler(connectMux, maxAPIRequestBytes)))

	return nil
}
