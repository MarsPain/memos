import { Code, ConnectError } from "@connectrpc/connect";
import { LoaderIcon, MessageSquareIcon, SendIcon } from "lucide-react";
import { useState } from "react";
import { toast } from "react-hot-toast";
import { Link } from "react-router-dom";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { useChatConversation, useSendChatMessage } from "@/hooks/useAIQueries";
import { cn } from "@/lib/utils";
import { ROUTES } from "@/router";
import { ChatMessage_Role, type ChatMessage as ChatMessageType } from "@/types/proto/api/v1/ai_service_pb";
import { useTranslate } from "@/utils/i18n";

const ChatMessageBubble = ({ message }: { message: ChatMessageType }) => {
  const t = useTranslate();
  const isUser = message.role === ChatMessage_Role.USER;

  return (
    <div className={cn("flex w-full", isUser ? "justify-end" : "justify-start")}>
      <div className={cn("flex max-w-[80%] flex-col gap-1", isUser && "items-end")}>
        <span className="text-xs font-medium text-muted-foreground">{isUser ? t("chat.you") : t("chat.assistant")}</span>
        <div
          className={cn(
            "rounded-2xl px-4 py-2 text-sm whitespace-pre-wrap break-words",
            isUser ? "bg-primary text-primary-foreground" : "bg-muted text-foreground",
          )}
        >
          {message.content}
        </div>
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

const Chat = () => {
  const t = useTranslate();
  const conversationQuery = useChatConversation();
  const sendMessageMutation = useSendChatMessage();
  const [input, setInput] = useState("");

  const conversation = conversationQuery.data;
  const messages = conversation?.messages ?? [];
  const generationAvailable = conversation?.generationAvailable ?? false;
  const sendDisabled = input.trim() === "" || sendMessageMutation.isPending;

  const handleSend = () => {
    const content = input.trim();
    if (content === "" || sendMessageMutation.isPending) {
      return;
    }
    sendMessageMutation.mutate(content, {
      onSuccess: () => setInput(""),
      onError: (error) => {
        if (error instanceof ConnectError && error.code === Code.FailedPrecondition) {
          toast.error(t("chat.unavailable-description"));
        } else {
          toast.error(t("chat.send-failed"));
        }
      },
    });
  };

  if (conversationQuery.isPending) {
    return (
      <section className="mx-auto flex w-full max-w-3xl flex-col gap-4 pb-10">
        <div className="flex justify-center py-16">
          <LoaderIcon className="h-8 w-8 animate-spin text-muted-foreground" />
        </div>
      </section>
    );
  }

  return (
    <section className="mx-auto flex w-full max-w-3xl flex-col gap-4 pb-10">
      <div className="flex items-center gap-2 border-b border-border pb-4">
        <MessageSquareIcon className="h-5 w-5 text-muted-foreground" />
        <h1 className="text-2xl font-semibold tracking-normal text-foreground">{t("chat.title")}</h1>
      </div>

      {conversationQuery.isError ? (
        <div className="rounded-lg border border-dashed border-border px-4 py-10 text-center">
          <p className="text-sm text-muted-foreground">{t("chat.load-failed")}</p>
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
                <ChatMessageBubble key={index} message={message} />
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
    </section>
  );
};

export default Chat;
