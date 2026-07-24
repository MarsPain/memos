import { type QueryClient, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useRef, useState } from "react";
import { aiServiceClient } from "@/connect";
import { type ChatConversation, type ChatMessage, ChatMessage_Role, ChatMessage_Status } from "@/types/proto/api/v1/ai_service_pb";

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

// upsertChatMessages inserts the persisted send pair into the cached
// conversation, deduplicating against refetches racing the stream. Attempt
// numbers restart per user message, so the assistant attempt dedupes relative
// to its own user message.
function upsertChatMessages(queryClient: QueryClient, conversationName: string, userMessage?: ChatMessage, assistantMessage?: ChatMessage) {
  if (!userMessage || !assistantMessage) {
    return;
  }
  queryClient.setQueryData(aiKeys.chatConversation(conversationName), (conversation: ChatConversation | undefined) => {
    if (!conversation) {
      return conversation;
    }
    const messages = [...conversation.messages];
    const userIndex = messages.findIndex(
      (message) => message.clientRequestId !== "" && message.clientRequestId === userMessage.clientRequestId,
    );
    if (userIndex === -1) {
      messages.push(userMessage, assistantMessage);
    } else if (
      !messages
        .slice(userIndex + 1)
        .some((message) => message.role === ChatMessage_Role.ASSISTANT && message.attempt === assistantMessage.attempt)
    ) {
      messages.push(assistantMessage);
    }
    return { ...conversation, messages };
  });
}

// replaceChatMessage swaps the streamed attempt for the authoritative stored
// message carried by the terminal complete event.
function replaceChatMessage(queryClient: QueryClient, conversationName: string, complete: ChatMessage) {
  queryClient.setQueryData(aiKeys.chatConversation(conversationName), (conversation: ChatConversation | undefined) => {
    if (!conversation) {
      return conversation;
    }
    const messages = [...conversation.messages];
    for (let index = messages.length - 1; index >= 0; index--) {
      const message = messages[index];
      if (
        message.role === ChatMessage_Role.ASSISTANT &&
        message.status === ChatMessage_Status.STREAMING &&
        message.attempt === complete.attempt
      ) {
        messages[index] = complete;
        return { ...conversation, messages };
      }
    }
    messages.push(complete);
    return { ...conversation, messages };
  });
}

// useSendChatMessageStream streams one chat send: the answer renders
// incrementally from deltas while streaming state stays local to the active
// response, and the cached conversation reconciles with the authoritative
// stored message at completion. Aborting the stream (conversation switch or
// unmount) propagates the disconnect to the server, which persists the
// attempt as CANCELLED.
export function useSendChatMessageStream(conversationName: string | undefined) {
  const queryClient = useQueryClient();
  const [streamedContent, setStreamedContent] = useState("");
  const [isStreaming, setIsStreaming] = useState(false);
  const abortRef = useRef<AbortController | undefined>(undefined);

  useEffect(() => {
    return () => abortRef.current?.abort();
  }, [conversationName]);

  const send = useCallback(
    async (input: { content: string; requestId: string }, onStart?: () => void) => {
      if (!conversationName) {
        return;
      }
      const controller = new AbortController();
      abortRef.current = controller;
      setIsStreaming(true);
      setStreamedContent("");
      try {
        const stream = aiServiceClient.sendChatMessage(
          { conversation: conversationName, content: input.content, requestId: input.requestId },
          { signal: controller.signal },
        );
        for await (const event of stream) {
          if (event.event.case === "start") {
            upsertChatMessages(queryClient, conversationName, event.event.value.userMessage, event.event.value.assistantMessage);
            onStart?.();
          } else if (event.event.case === "delta") {
            setStreamedContent((previous) => previous + event.event.value);
          } else if (event.event.case === "complete") {
            replaceChatMessage(queryClient, conversationName, event.event.value);
          }
        }
        // Reconcile with the authoritative stored state; the list ordering
        // and the derived conversation title may have changed too.
        queryClient.invalidateQueries({ queryKey: aiKeys.chatConversation(conversationName) });
        queryClient.invalidateQueries({ queryKey: aiKeys.chatConversations() });
      } catch (error) {
        if (controller.signal.aborted) {
          return;
        }
        // Reconcile so a persisted FAILED or CANCELLED attempt renders.
        queryClient.invalidateQueries({ queryKey: aiKeys.chatConversation(conversationName) });
        throw error;
      } finally {
        if (abortRef.current === controller) {
          abortRef.current = undefined;
        }
        setIsStreaming(false);
        setStreamedContent("");
      }
    },
    [conversationName, queryClient],
  );

  return { send, streamedContent, isStreaming };
}
