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

const renderChat = () =>
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <MemoryRouter initialEntries={["/chat"]}>
        <Chat />
      </MemoryRouter>
    </QueryClientProvider>,
  );

describe("<Chat>", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.listChatConversations.mockResolvedValue(conversationList());
    mocks.getChatConversation.mockResolvedValue(conversation());
    mocks.createChatConversation.mockResolvedValue(conversation({ name: "ai/conversations/c2", title: "" }));
    mocks.deleteChatConversation.mockResolvedValue({});
    mocks.sendChatMessage.mockResolvedValue({});
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
          { role: ChatMessage_Role.USER, content: "question", status: ChatMessage_Status.COMPLETE, citations: [] },
          { role: ChatMessage_Role.ASSISTANT, content: "", status: ChatMessage_Status.FAILED, attempt: 1, citations: [] },
        ],
      }),
    );

    renderChat();

    expect(await screen.findByText("chat.answer-failed")).toBeInTheDocument();
  });

  it("sends the composer content with the active conversation and a client request ID", async () => {
    renderChat();

    const composer = await screen.findByPlaceholderText("chat.input-placeholder");
    fireEvent.change(composer, { target: { value: "hello memos" } });
    fireEvent.keyDown(composer, { key: "Enter", shiftKey: false });

    await waitFor(() =>
      expect(mocks.sendChatMessage).toHaveBeenCalledWith({
        conversation: "ai/conversations/c1",
        content: "hello memos",
        requestId: expect.any(String),
      }),
    );
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
