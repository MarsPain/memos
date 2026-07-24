import { Code, ConnectError } from "@connectrpc/connect";
import { LoaderIcon, MessageSquareIcon, PlusIcon, RotateCcwIcon, SendIcon, Trash2Icon } from "lucide-react";
import { useState } from "react";
import { toast } from "react-hot-toast";
import { Link } from "react-router-dom";
import ConfirmDialog from "@/components/ConfirmDialog";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import {
  useChatConversation,
  useChatConversations,
  useCreateChatConversation,
  useDeleteChatConversation,
  useSendChatMessageStream,
} from "@/hooks/useAIQueries";
import useDialog from "@/hooks/useDialog";
import { cn } from "@/lib/utils";
import { ROUTES } from "@/router";
import { ChatMessage_Role, ChatMessage_Status, type ChatMessage as ChatMessageType } from "@/types/proto/api/v1/ai_service_pb";
import { useTranslate } from "@/utils/i18n";

const ChatMessageBubble = ({
  message,
  streamedContent,
  onRetry,
}: {
  message: ChatMessageType;
  // Incremental answer text of the active stream; overlays the empty content
  // of the attempt still generating.
  streamedContent?: string;
  onRetry?: () => void;
}) => {
  const t = useTranslate();
  const isUser = message.role === ChatMessage_Role.USER;
  const content = streamedContent ?? message.content;

  return (
    <div className={cn("flex w-full", isUser ? "justify-end" : "justify-start")}>
      <div className={cn("flex max-w-[80%] flex-col gap-1", isUser && "items-end")}>
        <span className="text-xs font-medium text-muted-foreground">{isUser ? t("chat.you") : t("chat.assistant")}</span>
        {content !== "" && (
          <div
            className={cn(
              "rounded-2xl px-4 py-2 text-sm whitespace-pre-wrap break-words",
              isUser ? "bg-primary text-primary-foreground" : "bg-muted text-foreground",
            )}
          >
            {content}
          </div>
        )}
        {!isUser && message.status === ChatMessage_Status.STREAMING && (
          <span className="text-xs text-muted-foreground">{t("chat.generating")}</span>
        )}
        {!isUser && (message.status === ChatMessage_Status.FAILED || message.status === ChatMessage_Status.CANCELLED) && (
          <div className="flex items-center gap-2">
            <span className={cn("text-xs", message.status === ChatMessage_Status.FAILED ? "text-destructive" : "text-muted-foreground")}>
              {message.status === ChatMessage_Status.FAILED ? t("chat.answer-failed") : t("chat.answer-cancelled")}
            </span>
            {onRetry && (
              <Button variant="outline" size="sm" className="h-6 gap-1 px-2 text-xs" onClick={onRetry}>
                <RotateCcwIcon className="h-3 w-3" />
                {t("chat.retry")}
              </Button>
            )}
          </div>
        )}
        {message.citations.length > 0 && (
          <div className="flex flex-wrap gap-1">
            {message.citations.map((citation) => (
              <Link
                key={citation.memo}
                to={`/${citation.memo}`}
                viewTransition
                title={citation.snippet}
                className="inline-flex items-center gap-1 rounded-md border border-border bg-background px-2 py-0.5 text-xs text-muted-foreground hover:bg-accent hover:text-accent-foreground"
              >
                {citation.memo.split("/").pop()}
              </Link>
            ))}
          </div>
        )}
      </div>
    </div>
  );
};

// computeRetryTargets maps the index of each retryable assistant message to
// the user message it answers. An attempt is retryable when it failed or was
// cancelled and is the latest attempt of its user message; retries resend
// the same request ID.
const computeRetryTargets = (messages: ChatMessageType[]): Map<number, ChatMessageType> => {
  const userByAttemptIndex = new Map<number, ChatMessageType>();
  let lastUserMessage: ChatMessageType | undefined;
  messages.forEach((message, index) => {
    if (message.role === ChatMessage_Role.USER) {
      lastUserMessage = message;
    } else if (lastUserMessage) {
      userByAttemptIndex.set(index, lastUserMessage);
    }
  });
  const targets = new Map<number, ChatMessageType>();
  let sawAssistant = false;
  for (let index = messages.length - 1; index >= 0; index--) {
    const message = messages[index];
    if (message.role === ChatMessage_Role.ASSISTANT) {
      const userMessage = userByAttemptIndex.get(index);
      if (
        !sawAssistant &&
        (message.status === ChatMessage_Status.FAILED || message.status === ChatMessage_Status.CANCELLED) &&
        userMessage
      ) {
        targets.set(index, userMessage);
      }
      sawAssistant = true;
    } else {
      sawAssistant = false;
    }
  }
  return targets;
};

const Chat = () => {
  const t = useTranslate();
  const conversationsQuery = useChatConversations();
  const [selectedName, setSelectedName] = useState<string | undefined>(undefined);

  const conversations = conversationsQuery.data?.conversations ?? [];
  const generationAvailable = conversationsQuery.data?.generationAvailable ?? false;
  // The selection falls back to the most recently updated conversation and
  // recovers when the selected conversation is deleted.
  const activeName =
    selectedName && conversations.some((conversation) => conversation.name === selectedName) ? selectedName : conversations[0]?.name;

  const conversationQuery = useChatConversation(activeName);
  const createConversationMutation = useCreateChatConversation();
  const deleteConversationMutation = useDeleteChatConversation();
  const sendStream = useSendChatMessageStream(activeName);
  const deleteDialog = useDialog();
  const [deletingName, setDeletingName] = useState<string | undefined>(undefined);
  const [input, setInput] = useState("");

  const messages = conversationQuery.data?.messages ?? [];
  const sendDisabled = input.trim() === "" || !activeName || sendStream.isStreaming;

  // The active stream renders its deltas into the bubble of the attempt
  // still generating, which the start event inserted as the last message.
  const activeAttemptIndex =
    sendStream.isStreaming && messages.length > 0 && messages[messages.length - 1].status === ChatMessage_Status.STREAMING
      ? messages.length - 1
      : -1;

  // A failed or cancelled attempt can be retried only when it is the latest
  // attempt of its user message; retries resend the same request ID.
  const retryTargets = computeRetryTargets(messages);

  const handleNewChat = () => {
    createConversationMutation.mutate(undefined, {
      onSuccess: (created) => setSelectedName(created.name),
      onError: () => toast.error(t("chat.create-failed")),
    });
  };

  const handleDelete = async () => {
    if (!deletingName) {
      return;
    }
    try {
      await deleteConversationMutation.mutateAsync(deletingName);
      if (deletingName === selectedName) {
        setSelectedName(undefined);
      }
    } catch (error) {
      toast.error(t("chat.delete-failed"));
      throw error;
    }
  };

  const handleSendError = (error: unknown) => {
    if (error instanceof ConnectError && error.code === Code.FailedPrecondition) {
      toast.error(t("chat.unavailable-description"));
    } else if (error instanceof ConnectError && error.code === Code.ResourceExhausted) {
      toast.error(t("chat.rate-limited"));
    } else {
      toast.error(t("chat.send-failed"));
    }
  };

  const handleSend = () => {
    const content = input.trim();
    if (content === "" || !activeName || sendStream.isStreaming) {
      return;
    }
    // The input clears once the message is durably stored (the start event),
    // so a rejected send loses nothing.
    sendStream.send({ content, requestId: crypto.randomUUID() }, () => setInput("")).catch(handleSendError);
  };

  const handleRetry = (userMessage: ChatMessageType) => {
    if (!activeName || sendStream.isStreaming) {
      return;
    }
    sendStream.send({ content: userMessage.content, requestId: userMessage.clientRequestId }).catch(handleSendError);
  };

  if (conversationsQuery.isPending) {
    return (
      <section className="mx-auto flex w-full max-w-5xl flex-col gap-4 pb-10">
        <div className="flex justify-center py-16">
          <LoaderIcon className="h-8 w-8 animate-spin text-muted-foreground" />
        </div>
      </section>
    );
  }

  return (
    <section className="mx-auto flex w-full max-w-5xl flex-col gap-4 pb-10">
      <div className="flex items-center gap-2 border-b border-border pb-4">
        <MessageSquareIcon className="h-5 w-5 text-muted-foreground" />
        <h1 className="text-2xl font-semibold tracking-normal text-foreground">{t("chat.title")}</h1>
      </div>

      {conversationsQuery.isError ? (
        <div className="rounded-lg border border-dashed border-border px-4 py-10 text-center">
          <p className="text-sm text-muted-foreground">{t("chat.load-failed")}</p>
        </div>
      ) : (
        <div className="flex flex-row items-start gap-4">
          <aside className="flex w-56 shrink-0 flex-col gap-2">
            <Button
              variant="outline"
              className="w-full justify-start"
              onClick={handleNewChat}
              disabled={createConversationMutation.isPending}
            >
              <PlusIcon className="h-4 w-4" />
              {t("chat.new-chat")}
            </Button>
            <div className="flex flex-col gap-0.5">
              {conversations.map((conversation) => (
                <div
                  key={conversation.name}
                  role="button"
                  tabIndex={0}
                  onClick={() => setSelectedName(conversation.name)}
                  onKeyDown={(event) => {
                    if (event.key === "Enter") {
                      setSelectedName(conversation.name);
                    }
                  }}
                  className={cn(
                    "group flex cursor-pointer items-center gap-1 rounded-md px-2 py-1.5 text-sm",
                    conversation.name === activeName ? "bg-accent text-accent-foreground" : "text-muted-foreground hover:bg-accent/50",
                  )}
                >
                  <span className="flex-1 truncate">{conversation.title || t("chat.untitled")}</span>
                  <button
                    type="button"
                    aria-label={t("common.delete")}
                    className="shrink-0 rounded p-0.5 opacity-0 hover:bg-destructive/10 hover:text-destructive group-hover:opacity-100"
                    onClick={(event) => {
                      event.stopPropagation();
                      setDeletingName(conversation.name);
                      deleteDialog.open();
                    }}
                  >
                    <Trash2Icon className="h-3.5 w-3.5" />
                  </button>
                </div>
              ))}
            </div>
          </aside>

          <div className="flex min-w-0 flex-1 flex-col gap-4">
            {!activeName ? (
              <div className="rounded-lg border border-dashed border-border px-4 py-10 text-center">
                <p className="text-sm text-muted-foreground">{t("chat.no-conversations")}</p>
              </div>
            ) : conversationQuery.isPending ? (
              <div className="flex justify-center py-10">
                <LoaderIcon className="h-6 w-6 animate-spin text-muted-foreground" />
              </div>
            ) : (
              <>
                {messages.length === 0 ? (
                  generationAvailable && (
                    <div className="rounded-lg border border-dashed border-border px-4 py-10 text-center">
                      <p className="text-sm text-muted-foreground">{t("chat.empty")}</p>
                    </div>
                  )
                ) : (
                  <div className="flex flex-col gap-4">
                    {messages.map((message, index) => (
                      <ChatMessageBubble
                        key={message.clientRequestId || `${message.role}-${index}`}
                        message={message}
                        streamedContent={index === activeAttemptIndex ? sendStream.streamedContent : undefined}
                        onRetry={
                          retryTargets.has(index) && !sendStream.isStreaming
                            ? () => handleRetry(retryTargets.get(index) as ChatMessageType)
                            : undefined
                        }
                      />
                    ))}
                  </div>
                )}

                {generationAvailable ? (
                  <form
                    className="flex flex-row items-end gap-2"
                    onSubmit={(event) => {
                      event.preventDefault();
                      handleSend();
                    }}
                  >
                    <Textarea
                      value={input}
                      onChange={(event) => setInput(event.target.value)}
                      onKeyDown={(event) => {
                        if (event.key === "Enter" && !event.shiftKey) {
                          event.preventDefault();
                          handleSend();
                        }
                      }}
                      placeholder={t("chat.input-placeholder")}
                      aria-label={t("chat.input-placeholder")}
                      rows={1}
                      className="min-h-9 flex-1 resize-none"
                    />
                    <Button type="submit" disabled={sendDisabled}>
                      <SendIcon className="h-4 w-4" />
                      {t("chat.send")}
                    </Button>
                  </form>
                ) : (
                  <div className="rounded-lg border border-dashed border-border px-4 py-6 text-center">
                    <p className="text-sm font-medium text-foreground">{t("chat.unavailable-title")}</p>
                    <p className="mx-auto mt-1 max-w-md text-sm text-muted-foreground">{t("chat.unavailable-description")}</p>
                    <Link
                      to={`${ROUTES.SETTING}#ai`}
                      viewTransition
                      className="mt-3 inline-block text-sm font-medium text-primary hover:underline"
                    >
                      {t("chat.unavailable-settings-link")}
                    </Link>
                  </div>
                )}
              </>
            )}
          </div>
        </div>
      )}

      <ConfirmDialog
        open={deleteDialog.isOpen}
        onOpenChange={deleteDialog.setOpen}
        title={t("chat.delete-confirm")}
        description={t("chat.delete-confirm-description")}
        confirmLabel={t("common.delete")}
        cancelLabel={t("common.cancel")}
        onConfirm={handleDelete}
        confirmVariant="destructive"
      />
    </section>
  );
};

export default Chat;
