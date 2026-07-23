import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { aiServiceClient } from "@/connect";
import { ChatMessage_Status } from "@/types/proto/api/v1/ai_service_pb";

// Query keys factory for consistent cache management
export const aiKeys = {
  all: ["ai"] as const,
  chatConversations: () => [...aiKeys.all, "chat-conversations"] as const,
  chatConversation: (name: string) => [...aiKeys.all, "chat-conversation", name] as const,
};

export function useChatConversations() {
  return useQuery({
    queryKey: aiKeys.chatConversations(),
    queryFn: async () => {
      const response = await aiServiceClient.listChatConversations({});
      return response;
    },
  });
}

export function useChatConversation(name: string | undefined) {
  return useQuery({
    queryKey: aiKeys.chatConversation(name ?? ""),
    queryFn: async () => {
      const conversation = await aiServiceClient.getChatConversation({ name: name as string });
      return conversation;
    },
    enabled: Boolean(name),
    // While any attempt is still streaming, poll so the thread reconciles
    // with the authoritative stored state.
    refetchInterval: (query) =>
      query.state.data?.messages.some((message) => message.status === ChatMessage_Status.STREAMING) ? 2000 : false,
  });
}

export function useCreateChatConversation() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (title?: string) => {
      const conversation = await aiServiceClient.createChatConversation({ title: title ?? "" });
      return conversation;
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: aiKeys.chatConversations() });
    },
  });
}

export function useDeleteChatConversation() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (name: string) => {
      await aiServiceClient.deleteChatConversation({ name });
      return name;
    },
    onSuccess: (name) => {
      queryClient.removeQueries({ queryKey: aiKeys.chatConversation(name) });
      queryClient.invalidateQueries({ queryKey: aiKeys.chatConversations() });
    },
  });
}

export function useSendChatMessage() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (input: { conversation: string; content: string; requestId: string }) => {
      const response = await aiServiceClient.sendChatMessage(input);
      return response;
    },
    onSuccess: (_, input) => {
      // Refetch the conversation so it includes both the user message and the assistant reply,
      // and the list so ordering and derived titles update.
      queryClient.invalidateQueries({ queryKey: aiKeys.chatConversation(input.conversation) });
      queryClient.invalidateQueries({ queryKey: aiKeys.chatConversations() });
    },
  });
}
