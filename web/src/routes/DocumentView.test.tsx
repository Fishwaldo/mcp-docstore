import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import DocumentView from "@/routes/DocumentView";

vi.mock("@/lib/api", () => ({
  getDocument: vi.fn().mockResolvedValue({
    id: "d1",
    project_id: "p1",
    title: "Test Document",
    overview: "A test overview",
    tags: ["foo", "bar"],
    version: 3,
    change_comment: "updated",
    updated_at: "2024-06-01T00:00:00Z",
    body_html: "<p>Hello world</p>",
  }),
  listSnapshots: vi.fn().mockResolvedValue([
    {
      version: 1,
      comment: "initial",
      created_by: "u",
      created_at: "2024-01-01T00:00:00Z",
    },
    {
      version: 2,
      comment: "revision",
      created_by: "u",
      created_at: "2024-03-01T00:00:00Z",
    },
  ]),
}));

function wrapper({ children }: { children: React.ReactNode }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return (
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={["/documents/d1"]}>
        <Routes>
          <Route path="/documents/:id" element={children} />
          <Route path="/documents/:id/diff" element={<div>Diff page</div>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>
  );
}

describe("DocumentView", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders body HTML content", async () => {
    render(<DocumentView />, { wrapper });
    await waitFor(() => {
      expect(screen.getByText("Hello world")).toBeInTheDocument();
    });
  });

  it("renders tags", async () => {
    render(<DocumentView />, { wrapper });
    await waitFor(() => {
      expect(screen.getByText("foo")).toBeInTheDocument();
      expect(screen.getByText("bar")).toBeInTheDocument();
    });
  });

  it("renders version number", async () => {
    render(<DocumentView />, { wrapper });
    await waitFor(() => {
      expect(screen.getByText("3")).toBeInTheDocument();
    });
  });

  it("collapses and expands the overview via its toggle", async () => {
    render(<DocumentView />, { wrapper });
    await waitFor(() => {
      expect(screen.getByText("A test overview")).toBeInTheDocument();
    });
    const toggle = screen.getByRole("button", { name: /overview/i });
    fireEvent.click(toggle);
    expect(screen.queryByText("A test overview")).not.toBeInTheDocument();
    fireEvent.click(toggle);
    expect(screen.getByText("A test overview")).toBeInTheDocument();
  });

  it("renders diff links for snapshots", async () => {
    render(<DocumentView />, { wrapper });
    await waitFor(() => {
      const diffLinks = screen.getAllByText("Diff vs current");
      expect(diffLinks.length).toBeGreaterThan(0);
      expect(diffLinks[0].closest("a")).toHaveAttribute(
        "href",
        expect.stringContaining("/diff?from=")
      );
    });
  });
});
