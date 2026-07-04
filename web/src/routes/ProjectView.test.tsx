import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import ProjectView from "@/routes/ProjectView";
import { getProject, listDocuments } from "@/lib/api";

vi.mock("@/lib/api", async () => {
  const actual = await vi.importActual<typeof import("@/lib/api")>("@/lib/api");
  return {
    ...actual,
    getProject: vi.fn(),
    listDocuments: vi.fn(),
  };
});

function wrapper({ children }: { children: React.ReactNode }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return (
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={["/projects/p1"]}>
        <Routes>
          <Route path="/projects/:id" element={children} />
          <Route path="/documents/:id" element={<div>Document page</div>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>
  );
}

describe("ProjectView", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders project metadata, badges, and document list", async () => {
    vi.mocked(getProject).mockResolvedValue({
      id: "p1",
      name: "Alpha Project",
      description: "desc",
      visibility: "org",
      archived: false,
      access: "write",
    });
    vi.mocked(listDocuments).mockResolvedValue([
      {
        id: "d1",
        title: "Doc One",
        overview: "ov",
        tags: [],
        version: 1,
        updated_at: "2026-01-01T00:00:00Z",
      },
    ]);

    render(<ProjectView />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("Alpha Project")).toBeInTheDocument();
    });
    expect(screen.getByText("Org")).toBeInTheDocument();
    expect(screen.getByText("write")).toBeInTheDocument();

    const link = screen.getByText("Doc One").closest("a");
    expect(link).toHaveAttribute("href", "/documents/d1");
  });

  it("shows a not-found message when the project fails to load", async () => {
    vi.mocked(getProject).mockRejectedValue(new Error("API error 404: not found"));
    vi.mocked(listDocuments).mockResolvedValue([]);

    render(<ProjectView />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText(/not found/i)).toBeInTheDocument();
    });
  });
});
