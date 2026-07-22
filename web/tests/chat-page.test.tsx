import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import Chat from "@/pages/Chat";
import { ChatMessage_Role } from "@/types/proto/api/v1/ai_service_pb";

const mocks = vi.hoisted(() => ({
  getChatConversation: vi.fn(),
  sendChatMessage: vi.fn(),
}));

vi.mock("@/connect", () => ({
  aiServiceClient: {
    getChatConversation: mocks.getChatConversation,
    sendChatMessage: mocks.sendChatMessage,
  },
}));

vi.mock("@/utils/i18n", () => ({
  useTranslate: () => (key: string) => key,
  // `@/i18n` (pulled in transitively via `@/router`) calls this from its locale loader.
  findNearestMatchedLanguage: () => "en",
}));

const conversation = (overrides: Record<string, unknown> = {}) => ({
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
    mocks.getChatConversation.mockResolvedValue(conversation());
    mocks.sendChatMessage.mockResolvedValue({});
  });

  it("renders conversation messages and citation chips linking to source memos", async () => {
    mocks.getChatConversation.mockResolvedValue(
      conversation({
        messages: [
          { role: ChatMessage_Role.USER, content: "What did I note about penguins?", citations: [] },
          {
            role: ChatMessage_Role.ASSISTANT,
            content: "You noted that penguins are flightless birds.",
            citations: [{ memo: "memos/abc123", snippet: "Penguins are flightless birds." }],
          },
        ],
      }),
    );

    renderChat();

    expect(await screen.findByText("What did I note about penguins?")).toBeInTheDocument();
    expect(screen.getByText("You noted that penguins are flightless birds.")).toBeInTheDocument();

    const citationChip = screen.getByRole("link", { name: "abc123" });
    expect(citationChip).toHaveAttribute("href", "/memos/abc123");
    expect(citationChip).toHaveAttribute("title", "Penguins are flightless birds.");
  });

  it("sends the composer content through the AI service", async () => {
    renderChat();

    const composer = await screen.findByPlaceholderText("chat.input-placeholder");
    fireEvent.change(composer, { target: { value: "hello memos" } });
    fireEvent.keyDown(composer, { key: "Enter", shiftKey: false });

    await waitFor(() => expect(mocks.sendChatMessage).toHaveBeenCalledWith({ content: "hello memos" }));
  });

  it("shows the unavailable state without a composer when no generation model is configured", async () => {
    mocks.getChatConversation.mockResolvedValue(conversation({ generationAvailable: false }));

    renderChat();

    expect(await screen.findByText("chat.unavailable-title")).toBeInTheDocument();
    expect(screen.getByText("chat.unavailable-description")).toBeInTheDocument();
    expect(screen.queryByPlaceholderText("chat.input-placeholder")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "chat.send" })).not.toBeInTheDocument();
  });
});
