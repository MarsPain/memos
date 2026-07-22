import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { aiServiceClient } from "@/connect";

// Query keys factory for consistent cache management
export const aiKeys = {
  all: ["ai"] as const,
  chatConversation: () => [...aiKeys.all, "chat-conversation"] as const,
};

export function useChatConversation() {
  return useQuery({
    queryKey: aiKeys.chatConversation(),
    queryFn: async () => {
      const conversation = await aiServiceClient.getChatConversation({});
      return conversation;
    },
  });
}

export function useSendChatMessage() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (content: string) => {
      const response = await aiServiceClient.sendChatMessage({ content });
      return response;
    },
    onSuccess: () => {
      // Refetch the conversation so it includes both the user message and the assistant reply.
      queryClient.invalidateQueries({ queryKey: aiKeys.chatConversation() });
    },
  });
}
