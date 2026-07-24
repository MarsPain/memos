import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import Chat from "@/pages/Chat";
import { ChatMessage_Role, ChatMessage_Status } from "@/types/proto/api/v1/ai_service_pb";

const mocks = vi.hoisted(() => ({
  listChatConversations: vi.fn(),
  getChatConversation: vi.fn(),
  createChatConversation: vi.fn(),
  deleteChatConversation: vi.fn(),
  sendChatMessage: vi.fn(),
}));

vi.mock("@/connect", () => ({
  aiServiceClient: {
    listChatConversations: mocks.listChatConversations,
    getChatConversation: mocks.getChatConversation,
    createChatConversation: mocks.createChatConversation,
    deleteChatConversation: mocks.deleteChatConversation,
    sendChatMessage: mocks.sendChatMessage,
  },
}));

vi.mock("@/utils/i18n", () => ({
  useTranslate: () => (key: string) => key,
  // `@/i18n` (pulled in transitively via `@/router`) calls this from its locale loader.
  findNearestMatchedLanguage: () => "en",
}));

const conversationList = (overrides: Record<string, unknown> = {}) => ({
  generationAvailable: true,
  conversations: [{ name: "ai/conversations/c1", title: "First conversation" }],
  ...overrides,
});

const conversation = (overrides: Record<string, unknown> = {}) => ({
  name: "ai/conversations/c1",
  title: "First conversation",
  generationAvailable: true,
  messages: [],
  ...overrides,
});

const userMessage = (overrides: Record<string, unknown> = {}) => ({
  role: ChatMessage_Role.USER,
  content: "question",
  status: ChatMessage_Status.COMPLETE,
  clientRequestId: "req-1",
  citations: [],
  ...overrides,
});

const assistantMessage = (overrides: Record<string, unknown> = {}) => ({
  role: ChatMessage_Role.ASSISTANT,
  content: "",
  status: ChatMessage_Status.STREAMING,
  attempt: 1,
  citations: [],
  ...overrides,
});

const startEvent = (user: Record<string, unknown>, assistant: Record<string, unknown>) => ({
  event: { case: "start", value: { userMessage: user, assistantMessage: assistant } },
});

const deltaEvent = (delta: string) => ({ event: { case: "delta", value: delta } });

const completeEvent = (assistant: Record<string, unknown>) => ({ event: { case: "complete", value: assistant } });

// streamOf returns an async iterable of the given events, mimicking a
// server-streaming Connect call.
async function* streamOf(events: Array<Record<string, unknown>>) {
  for (const event of events) {
    yield event;
  }
}

// createControlledStream returns a push-driven async iterable so tests can
// observe intermediate rendering states mid-stream.
const createControlledStream = () => {
  const queue: Array<Record<string, unknown>> = [];
  let closed = false;
  let waiter: (() => void) | undefined;
  const notifyWaiter = () => {
    const pending = waiter;
    waiter = undefined;
    pending?.();
  };
  const stream = (async function* () {
    for (let index = 0; ; ) {
      if (index < queue.length) {
        yield queue[index++];
        continue;
      }
      if (closed) {
        return;
      }
      await new Promise<void>((resolve) => {
        waiter = resolve;
      });
    }
  })();
  return {
    stream,
    push: (event: Record<string, unknown>) => {
      queue.push(event);
      notifyWaiter();
    },
    close: () => {
      closed = true;
      notifyWaiter();
    },
  };
};

const renderChat = () =>
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <MemoryRouter initialEntries={["/chat"]}>
        <Chat />
      </MemoryRouter>
    </QueryClientProvider>,
  );

// sendFromComposer types into the composer and submits.
const sendFromComposer = async (content: string) => {
  const composer = await screen.findByPlaceholderText("chat.input-placeholder");
  fireEvent.change(composer, { target: { value: content } });
  fireEvent.keyDown(composer, { key: "Enter", shiftKey: false });
};

describe("<Chat>", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.listChatConversations.mockResolvedValue(conversationList());
    mocks.getChatConversation.mockResolvedValue(conversation());
    mocks.createChatConversation.mockResolvedValue(conversation({ name: "ai/conversations/c2", title: "" }));
    mocks.deleteChatConversation.mockResolvedValue({});
    mocks.sendChatMessage.mockImplementation(() => streamOf([]));
  });

  it("renders the conversation list and the active conversation messages with citation chips", async () => {
    mocks.getChatConversation.mockResolvedValue(
      conversation({
        messages: [
          { role: ChatMessage_Role.USER, content: "What did I note about penguins?", status: ChatMessage_Status.COMPLETE, citations: [] },
          {
            role: ChatMessage_Role.ASSISTANT,
            content: "You noted that penguins are flightless birds.",
            status: ChatMessage_Status.COMPLETE,
            attempt: 1,
            citations: [{ memo: "memos/abc123", snippet: "Penguins are flightless birds." }],
          },
        ],
      }),
    );

    renderChat();

    expect(await screen.findByText("First conversation")).toBeInTheDocument();
    expect(await screen.findByText("What did I note about penguins?")).toBeInTheDocument();
    expect(screen.getByText("You noted that penguins are flightless birds.")).toBeInTheDocument();

    const citationChip = screen.getByRole("link", { name: "abc123" });
    expect(citationChip).toHaveAttribute("href", "/memos/abc123");
    expect(citationChip).toHaveAttribute("title", "Penguins are flightless birds.");
  });

  it("renders a failed-attempt hint on failed assistant messages", async () => {
    mocks.getChatConversation.mockResolvedValue(
      conversation({
        messages: [
          userMessage(),
          assistantMessage({ status: ChatMessage_Status.FAILED }),
        ],
      }),
    );

    renderChat();

    expect(await screen.findByText("chat.answer-failed")).toBeInTheDocument();
  });

  it("sends the composer content with the active conversation and a client request ID", async () => {
    renderChat();

    await sendFromComposer("hello memos");

    await waitFor(() =>
      expect(mocks.sendChatMessage).toHaveBeenCalledWith(
        {
          conversation: "ai/conversations/c1",
          content: "hello memos",
          requestId: expect.any(String),
        },
        expect.objectContaining({ signal: expect.any(AbortSignal) }),
      ),
    );
  });

  it("renders tokens incrementally and reconciles with the stored message at completion", async () => {
    const controlled = createControlledStream();
    mocks.sendChatMessage.mockImplementation(() => controlled.stream);
    const storedMessages = [
      userMessage(),
      assistantMessage({
        content: "Penguins are flightless birds.",
        status: ChatMessage_Status.COMPLETE,
        citations: [{ memo: "memos/abc123", snippet: "Penguins are flightless birds." }],
      }),
    ];

    renderChat();
    await sendFromComposer("question");
    await waitFor(() => expect(mocks.sendChatMessage).toHaveBeenCalled());

    // The persisted pair renders once the start event arrives.
    controlled.push(startEvent(userMessage(), assistantMessage()));
    expect(await screen.findByText("question")).toBeInTheDocument();
    expect(screen.getByText("chat.generating")).toBeInTheDocument();

    // Deltas render incrementally into the active attempt's bubble.
    controlled.push(deltaEvent("Penguins are "));
    expect(await screen.findByText("Penguins are")).toBeInTheDocument();
    expect(screen.queryByText("Penguins are flightless birds.")).not.toBeInTheDocument();
    controlled.push(deltaEvent("flightless birds."));
    expect(await screen.findByText("Penguins are flightless birds.")).toBeInTheDocument();

    // The terminal event swaps in the authoritative stored message and the
    // conversation refetch converges to the same persisted state.
    controlled.push(completeEvent(storedMessages[1]));
    mocks.getChatConversation.mockResolvedValue(conversation({ messages: storedMessages }));
    controlled.close();

    expect(await screen.findByRole("link", { name: "abc123" })).toBeInTheDocument();
    expect(screen.getByText("Penguins are flightless birds.")).toBeInTheDocument();
    await waitFor(() => expect(screen.queryByText("chat.generating")).not.toBeInTheDocument());
  });

  it("reconciles to the stored failed attempt when the stream fails", async () => {
    const controlled = createControlledStream();
    mocks.sendChatMessage.mockImplementation(() => controlled.stream);
    const storedMessages = [userMessage(), assistantMessage({ status: ChatMessage_Status.FAILED })];

    renderChat();
    await sendFromComposer("question");
    await waitFor(() => expect(mocks.sendChatMessage).toHaveBeenCalled());

    controlled.push(startEvent(userMessage(), assistantMessage()));
    controlled.push(completeEvent(storedMessages[1]));
    mocks.getChatConversation.mockResolvedValue(conversation({ messages: storedMessages }));
    controlled.close();

    expect(await screen.findByText("chat.answer-failed")).toBeInTheDocument();
  });

  it("retries a failed attempt with the same request ID instead of duplicating the user message", async () => {
    mocks.getChatConversation.mockResolvedValue(
      conversation({
        messages: [userMessage(), assistantMessage({ status: ChatMessage_Status.FAILED })],
      }),
    );
    const controlled = createControlledStream();
    mocks.sendChatMessage.mockImplementation(() => controlled.stream);

    renderChat();

    fireEvent.click(await screen.findByRole("button", { name: "chat.retry" }));

    await waitFor(() =>
      expect(mocks.sendChatMessage).toHaveBeenCalledWith(
        { conversation: "ai/conversations/c1", content: "question", requestId: "req-1" },
        expect.objectContaining({ signal: expect.any(AbortSignal) }),
      ),
    );
    controlled.close();
  });

  it("aborts the stream when navigating away, propagating the disconnect", async () => {
    const controlled = createControlledStream();
    let capturedSignal: AbortSignal | undefined;
    mocks.sendChatMessage.mockImplementation((_input: unknown, options: { signal?: AbortSignal }) => {
      capturedSignal = options?.signal;
      return controlled.stream;
    });

    const { unmount } = renderChat();
    await sendFromComposer("hello");
    await waitFor(() => expect(capturedSignal).toBeDefined());

    unmount();

    expect(capturedSignal?.aborted).toBe(true);
  });

  it("creates a new conversation from the new chat button", async () => {
    renderChat();

    fireEvent.click(await screen.findByRole("button", { name: /chat.new-chat/ }));

    await waitFor(() => expect(mocks.createChatConversation).toHaveBeenCalledWith({ title: "" }));
  });

  it("deletes a conversation after confirmation", async () => {
    renderChat();

    // The list item's delete button opens the confirmation dialog; the
    // dialog's own delete button confirms.
    fireEvent.click(await screen.findByRole("button", { name: "common.delete" }));
    const deleteButtons = await screen.findAllByRole("button", { name: "common.delete" });
    fireEvent.click(deleteButtons[deleteButtons.length - 1]);

    await waitFor(() => expect(mocks.deleteChatConversation).toHaveBeenCalledWith({ name: "ai/conversations/c1" }));
  });

  it("shows the empty state when there are no conversations", async () => {
    mocks.listChatConversations.mockResolvedValue(conversationList({ conversations: [] }));

    renderChat();

    expect(await screen.findByText("chat.no-conversations")).toBeInTheDocument();
  });

  it("shows the unavailable state without a composer when no generation model is configured", async () => {
    mocks.listChatConversations.mockResolvedValue(conversationList({ generationAvailable: false }));

    renderChat();

    expect(await screen.findByText("chat.unavailable-title")).toBeInTheDocument();
    expect(screen.getByText("chat.unavailable-description")).toBeInTheDocument();
    expect(screen.queryByPlaceholderText("chat.input-placeholder")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "chat.send" })).not.toBeInTheDocument();
  });
});
