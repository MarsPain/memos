import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import Search from "@/pages/Search";
import { Visibility } from "@/types/proto/api/v1/memo_service_pb";

const mocks = vi.hoisted(() => ({
  searchMemos: vi.fn(),
}));

vi.mock("@/connect", () => ({
  aiServiceClient: {
    searchMemos: mocks.searchMemos,
  },
}));

vi.mock("@/utils/i18n", () => ({
  useTranslate: () => (key: string, params?: { reasons?: string }) => (params?.reasons ? `${key}:${params.reasons}` : key),
  // `@/i18n` (pulled in transitively via `@/router`) calls this from its locale loader.
  findNearestMatchedLanguage: () => "en",
}));

const renderPage = () => {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={["/search"]}>
        <Search />
      </MemoryRouter>
    </QueryClientProvider>,
  );
};

const submitSearch = (query: string) => {
  fireEvent.change(screen.getByLabelText("search.input-placeholder"), { target: { value: query } });
  fireEvent.click(screen.getByRole("button", { name: /search\.run/ }));
};

describe("Search page", () => {
  beforeEach(() => {
    mocks.searchMemos.mockResolvedValue({ results: [], partialReasons: [] });
  });

  it("submits the query and renders results with snippets and rank reasons", async () => {
    mocks.searchMemos.mockResolvedValue({
      results: [
        { memo: "memos/birding", snippet: "The owl hunts at dusk.", rankReasons: ["content_exact"] },
        { memo: "memos/typo", snippet: "barn owl behavior", rankReasons: ["content_partial", "content_fuzzy"] },
      ],
      partialReasons: [],
    });
    renderPage();

    submitSearch("owl");

    await waitFor(() => expect(screen.getByText("The owl hunts at dusk.")).toBeTruthy());
    expect(mocks.searchMemos).toHaveBeenCalledWith(expect.objectContaining({ query: "owl" }));
    expect(screen.getByText("memos/birding").closest("a")?.getAttribute("href")).toBe("/memos/birding");
    expect(screen.getByText("content_exact")).toBeTruthy();
    expect(screen.getByText("content_fuzzy")).toBeTruthy();
    expect(screen.queryByText("search.no-results")).toBeNull();
  });

  it("translates the structured filter inputs into the request", async () => {
    renderPage();

    fireEvent.change(screen.getByLabelText("search.filter-tags"), { target: { value: "#birds, owls" } });
    fireEvent.change(screen.getByLabelText("search.filter-creator"), { target: { value: "users/7" } });
    fireEvent.change(screen.getByLabelText("search.filter-created-after"), { target: { value: "2026-01-01" } });
    fireEvent.change(screen.getByLabelText("search.filter-created-before"), { target: { value: "2026-01-31" } });
    submitSearch("owl");

    await waitFor(() => expect(mocks.searchMemos).toHaveBeenCalled());
    const request = mocks.searchMemos.mock.calls[0][0];
    expect(request.query).toBe("owl");
    expect(request.filter.tags).toEqual(["birds", "owls"]);
    expect(request.filter.creator).toBe("users/7");
    expect(request.filter.visibility).toBe(Visibility.VISIBILITY_UNSPECIFIED);
    expect(request.filter.createdAfter).toBeTruthy();
    expect(request.filter.createdBefore).toBeTruthy();
    // The after bound starts its day; the before bound includes its day.
    expect(Number(request.filter.createdAfter.seconds)).toBeLessThanOrEqual(Number(request.filter.createdBefore.seconds));
  });

  it("shows the partial/degraded banner with its machine-readable reasons", async () => {
    mocks.searchMemos.mockResolvedValue({
      results: [{ memo: "memos/birding", snippet: "The owl hunts.", rankReasons: ["content_exact"] }],
      partialReasons: ["scan_budget_exhausted"],
    });
    renderPage();

    submitSearch("owl");

    await waitFor(() => expect(screen.getByRole("alert").textContent).toContain("scan_budget_exhausted"));
    expect(screen.getByText("The owl hunts.")).toBeTruthy();
  });

  it("shows the empty state when nothing matches", async () => {
    renderPage();

    submitSearch("nonexistent");

    await waitFor(() => expect(screen.getByText("search.no-results")).toBeTruthy());
  });

  it("shows the error state when the search fails", async () => {
    mocks.searchMemos.mockRejectedValue(new Error("unavailable"));
    renderPage();

    submitSearch("owl");

    await waitFor(() => expect(screen.getByText("search.load-failed")).toBeTruthy());
  });
});
