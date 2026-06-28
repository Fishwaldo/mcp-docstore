import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import SearchResults from "@/routes/SearchResults";

vi.mock("@/lib/api", () => ({
  searchDocuments: vi.fn().mockResolvedValue([
    {
      document_id: "d1",
      project_id: "p1",
      title: "My Doc",
      overview: "",
      score: 1,
      snippet: "hello <mark>world</mark>",
    },
  ]),
}));

function wrapper({ children }: { children: React.ReactNode }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return (
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={["/search?q=world"]}>
        <Routes>
          <Route path="/search" element={children} />
          <Route path="/documents/:id" element={<div>Document</div>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>
  );
}

describe("SearchResults", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders the document title as a link", async () => {
    render(<SearchResults />, { wrapper });
    await waitFor(() => {
      expect(screen.getByText("My Doc")).toBeInTheDocument();
    });
    expect(screen.getByText("My Doc").closest("a")).toHaveAttribute(
      "href",
      "/documents/d1"
    );
  });

  it("renders snippet with highlighted text", async () => {
    render(<SearchResults />, { wrapper });
    await waitFor(() => {
      expect(screen.getByText("world")).toBeInTheDocument();
    });
  });

  it("shows the search query in the heading", async () => {
    render(<SearchResults />, { wrapper });
    await waitFor(() => {
      expect(screen.getByText(/world/i)).toBeInTheDocument();
    });
  });
});
