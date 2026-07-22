package v1

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1pb "github.com/usememos/memos/proto/gen/api/v1"
	serverai "github.com/usememos/memos/server/ai"
)

// SendChatMessage answers one user message grounded in the caller's memos and
// returns the stored user and assistant messages.
func (s *APIV1Service) SendChatMessage(ctx context.Context, request *v1pb.SendChatMessageRequest) (*v1pb.SendChatMessageResponse, error) {
	user, err := s.fetchCurrentUser(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get current user: %v", err)
	}
	if user == nil {
		return nil, status.Errorf(codes.Unauthenticated, "user not authenticated")
	}

	userMessage, assistantMessage, err := s.chatService().SendMessage(ctx, user, request.GetContent())
	if err != nil {
		return nil, err
	}
	return &v1pb.SendChatMessageResponse{
		UserMessage:      convertChatMessageToProto(userMessage),
		AssistantMessage: convertChatMessageToProto(assistantMessage),
	}, nil
}

// GetChatConversation returns the caller's conversation with the assistant
// plus whether text generation is configured.
func (s *APIV1Service) GetChatConversation(ctx context.Context, _ *v1pb.GetChatConversationRequest) (*v1pb.ChatConversation, error) {
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
	messages := chatService.Conversation(user.ID)
	conversation := &v1pb.ChatConversation{
		Messages:            make([]*v1pb.ChatMessage, 0, len(messages)),
		GenerationAvailable: available,
	}
	for _, message := range messages {
		conversation.Messages = append(conversation.Messages, convertChatMessageToProto(message))
	}
	return conversation, nil
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
	citations := make([]*v1pb.ChatCitation, 0, len(message.Citations))
	for _, citation := range message.Citations {
		citations = append(citations, &v1pb.ChatCitation{
			Memo:    "memos/" + citation.MemoUID,
			Snippet: citation.Snippet,
		})
	}
	return &v1pb.ChatMessage{
		Role:       role,
		Content:    message.Content,
		CreateTime: timestamppb.New(message.CreateTime),
		Citations:  citations,
	}
}
