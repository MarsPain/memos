package v1

import (
	"context"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1pb "github.com/usememos/memos/proto/gen/api/v1"
	serverai "github.com/usememos/memos/server/ai"
)

// aiConversationNamePrefix is the resource name prefix of AI chat
// conversations: ai/conversations/{conversation}.
const aiConversationNamePrefix = "ai/conversations/"

// CreateChatConversation creates a conversation owned by the caller.
func (s *APIV1Service) CreateChatConversation(ctx context.Context, request *v1pb.CreateChatConversationRequest) (*v1pb.ChatConversation, error) {
	user, err := s.fetchCurrentUser(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get current user: %v", err)
	}
	if user == nil {
		return nil, status.Errorf(codes.Unauthenticated, "user not authenticated")
	}

	conversation, err := s.chatService().CreateConversation(ctx, user, request.GetTitle())
	if err != nil {
		return nil, err
	}
	return convertChatConversationToProto(conversation, nil, false), nil
}

// ListChatConversations lists the caller's conversations, most recently
// updated first, plus whether text generation is configured.
func (s *APIV1Service) ListChatConversations(ctx context.Context, _ *v1pb.ListChatConversationsRequest) (*v1pb.ListChatConversationsResponse, error) {
	user, err := s.fetchCurrentUser(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get current user: %v", err)
	}
	if user == nil {
		return nil, status.Errorf(codes.Unauthenticated, "user not authenticated")
	}

	chatService := s.chatService()
	available, err := chatService.GenerationAvailable(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to check generation availability: %v", err)
	}
	conversations, err := chatService.ListConversations(ctx, user)
	if err != nil {
		return nil, err
	}
	response := &v1pb.ListChatConversationsResponse{
		Conversations:       make([]*v1pb.ChatConversation, 0, len(conversations)),
		GenerationAvailable: available,
	}
	for _, conversation := range conversations {
		response.Conversations = append(response.Conversations, convertChatConversationToProto(conversation, nil, false))
	}
	return response, nil
}

// GetChatConversation returns one of the caller's conversations with its
// messages plus whether text generation is configured.
func (s *APIV1Service) GetChatConversation(ctx context.Context, request *v1pb.GetChatConversationRequest) (*v1pb.ChatConversation, error) {
	user, err := s.fetchCurrentUser(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get current user: %v", err)
	}
	if user == nil {
		return nil, status.Errorf(codes.Unauthenticated, "user not authenticated")
	}

	uid, err := extractAIConversationUID(request.GetName())
	if err != nil {
		return nil, err
	}
	chatService := s.chatService()
	available, err := chatService.GenerationAvailable(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to check generation availability: %v", err)
	}
	conversation, messages, err := chatService.GetConversation(ctx, user, uid)
	if err != nil {
		return nil, err
	}
	return convertChatConversationToProto(conversation, messages, available), nil
}

// DeleteChatConversation deletes one of the caller's conversations,
// cancelling an attempt that is still generating.
func (s *APIV1Service) DeleteChatConversation(ctx context.Context, request *v1pb.DeleteChatConversationRequest) (*emptypb.Empty, error) {
	user, err := s.fetchCurrentUser(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get current user: %v", err)
	}
	if user == nil {
		return nil, status.Errorf(codes.Unauthenticated, "user not authenticated")
	}

	uid, err := extractAIConversationUID(request.GetName())
	if err != nil {
		return nil, err
	}
	if err := s.chatService().DeleteConversation(ctx, user, uid); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

// SendChatMessage answers one user message grounded in the caller's memos and
// returns the stored user message and assistant attempt. Repeating a request
// ID returns the existing pair instead of duplicating the user message.
func (s *APIV1Service) SendChatMessage(ctx context.Context, request *v1pb.SendChatMessageRequest) (*v1pb.SendChatMessageResponse, error) {
	user, err := s.fetchCurrentUser(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get current user: %v", err)
	}
	if user == nil {
		return nil, status.Errorf(codes.Unauthenticated, "user not authenticated")
	}

	uid, err := extractAIConversationUID(request.GetConversation())
	if err != nil {
		return nil, err
	}
	userMessage, assistantMessage, err := s.chatService().SendMessage(ctx, user, uid, request.GetContent(), request.GetRequestId())
	if err != nil {
		return nil, err
	}
	return &v1pb.SendChatMessageResponse{
		UserMessage:      convertChatMessageToProto(userMessage),
		AssistantMessage: convertChatMessageToProto(assistantMessage),
	}, nil
}

// extractAIConversationUID parses the UID out of a conversation resource
// name of the form ai/conversations/{conversation}.
func extractAIConversationUID(name string) (string, error) {
	uid, ok := strings.CutPrefix(name, aiConversationNamePrefix)
	if !ok || uid == "" || strings.Contains(uid, "/") {
		return "", status.Errorf(codes.InvalidArgument, "invalid conversation name: %q", name)
	}
	return uid, nil
}

// convertChatConversationToProto maps a chat service conversation to its API
// proto form. Messages are populated only by GetChatConversation.
func convertChatConversationToProto(conversation *serverai.Conversation, messages []serverai.Message, generationAvailable bool) *v1pb.ChatConversation {
	result := &v1pb.ChatConversation{
		Name:                aiConversationNamePrefix + conversation.UID,
		Title:               conversation.Title,
		CreateTime:          timestamppb.New(conversation.CreateTime),
		UpdateTime:          timestamppb.New(conversation.UpdateTime),
		GenerationAvailable: generationAvailable,
		Messages:            make([]*v1pb.ChatMessage, 0, len(messages)),
	}
	for _, message := range messages {
		result.Messages = append(result.Messages, convertChatMessageToProto(message))
	}
	return result
}

// convertChatMessageToProto maps a chat service message to its API proto form.
func convertChatMessageToProto(message serverai.Message) *v1pb.ChatMessage {
	role := v1pb.ChatMessage_ROLE_UNSPECIFIED
	switch message.Role {
	case serverai.RoleUser:
		role = v1pb.ChatMessage_USER
	case serverai.RoleAssistant:
		role = v1pb.ChatMessage_ASSISTANT
	}
	messageStatus := v1pb.ChatMessage_STATUS_UNSPECIFIED
	switch message.Status {
	case serverai.StatusStreaming:
		messageStatus = v1pb.ChatMessage_STREAMING
	case serverai.StatusComplete:
		messageStatus = v1pb.ChatMessage_COMPLETE
	case serverai.StatusFailed:
		messageStatus = v1pb.ChatMessage_FAILED
	case serverai.StatusCancelled:
		messageStatus = v1pb.ChatMessage_CANCELLED
	}
	citations := make([]*v1pb.ChatCitation, 0, len(message.Citations))
	for _, citation := range message.Citations {
		citations = append(citations, &v1pb.ChatCitation{
			Memo:    "memos/" + citation.MemoUID,
			Snippet: citation.Snippet,
		})
	}
	return &v1pb.ChatMessage{
		Role:            role,
		Content:         message.Content,
		CreateTime:      timestamppb.New(message.CreateTime),
		Citations:       citations,
		Status:          messageStatus,
		Attempt:         message.Attempt,
		ClientRequestId: message.ClientRequestID,
	}
}
