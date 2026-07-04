import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import ProjectTree from "@/components/ProjectTree";

vi.mock("@/lib/api", () => ({
  listProjects: vi.fn().mockResolvedValue([
    { id: "p1", name: "Alpha Project", description: "", visibility: "org", archived: false },
  ]),
  listDocuments: vi.fn().mockResolvedValue([
    { id: "b", title: "Zeta", overview: "", tags: [], version: 1, updated_at: "2026-02-01T00:00:00Z" },
    { id: "a", title: "Alpha", overview: "", tags: [], version: 1, updated_at: "2026-01-01T00:00:00Z" },
  ]),
}));

function wrapper({ children }: { children: React.ReactNode }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return (
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={["/"]}>{children}</MemoryRouter>
    </QueryClientProvider>
  );
}

describe("ProjectTree document ordering", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
  });

  async function renderExpanded() {
    render(<ProjectTree />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("Alpha Project")).toBeInTheDocument();
    });
    fireEvent.click(screen.getByText("Alpha Project"));

    await waitFor(() => {
      expect(screen.getByText("Zeta")).toBeInTheDocument();
      expect(screen.getByText("Alpha")).toBeInTheDocument();
    });
  }

  function docLinkTitles() {
    return screen
      .getAllByRole("link")
      .filter((l) => /^\/documents\/(a|b)$/.test(l.getAttribute("href") ?? ""))
      .map((l) => l.textContent);
  }

  it("defaults to A-Z order", async () => {
    await renderExpanded();

    const titles = docLinkTitles();
    expect(titles.indexOf("Alpha")).toBeLessThan(titles.indexOf("Zeta"));
  });

  it("flips to Recent order (most-recently-updated first) on click", async () => {
    await renderExpanded();

    fireEvent.click(screen.getByRole("button", { name: "Recent" }));

    await waitFor(() => {
      const titles = docLinkTitles();
      expect(titles.indexOf("Zeta")).toBeLessThan(titles.indexOf("Alpha"));
    });

    expect(localStorage.getItem("docOrder")).toBe("recent");
  });
});
