import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import NewDocument from "@/routes/NewDocument";
import { listProjects, createDocument, listTags } from "@/lib/api";

vi.mock("@/components/MarkdownEditor", () => ({
  default: ({ markdown, onChange }: { markdown: string; onChange: (v: string) => void }) => (
    <textarea data-testid="md-editor" value={markdown} onChange={(e) => onChange(e.target.value)} />
  ),
}));

vi.mock("@/lib/api", () => ({
  listProjects: vi.fn(),
  createDocument: vi.fn(),
  listTags: vi.fn(),
}));

function wrapper({ children }: { children: React.ReactNode }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return (
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={["/documents/new"]}>
        <Routes>
          <Route path="/documents/new" element={children} />
          <Route path="/documents/:id" element={<div>Document page</div>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>
  );
}

describe("NewDocument", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(listTags).mockResolvedValue([]);
  });

  it("creates a document and navigates to it", async () => {
    vi.mocked(listProjects).mockResolvedValue([
      {
        id: "p1",
        name: "Write Project",
        description: "",
        visibility: "org",
        archived: false,
        access: "write",
      },
    ]);
    vi.mocked(createDocument).mockResolvedValue({
      id: "new1",
      project_id: "p1",
      title: "My Doc",
      overview: "",
      body: "Some body",
      tags: [],
      version: 1,
      change_comment: "",
      updated_at: "2024-06-01T00:00:00Z",
      body_html: "<p>Some body</p>",
    });

    render(<NewDocument />, { wrapper });

    await screen.findByText("Write Project");

    fireEvent.change(screen.getByLabelText(/title/i), { target: { value: "My Doc" } });
    fireEvent.change(screen.getByTestId("md-editor"), { target: { value: "Some body" } });
    fireEvent.click(screen.getByRole("button", { name: /create/i }));

    await waitFor(() => {
      expect(createDocument).toHaveBeenCalledWith(
        expect.objectContaining({
          project_id: "p1",
          title: "My Doc",
          body: "Some body",
        })
      );
    });

    await waitFor(() => {
      expect(screen.getByText("Document page")).toBeInTheDocument();
    });
  });

  it("only lists projects with write access as targets", async () => {
    vi.mocked(listProjects).mockResolvedValue([
      {
        id: "p1",
        name: "Write Project",
        description: "",
        visibility: "org",
        archived: false,
        access: "write",
      },
      {
        id: "p2",
        name: "Read Project",
        description: "",
        visibility: "org",
        archived: false,
        access: "read",
      },
    ]);

    render(<NewDocument />, { wrapper });

    await screen.findByText("Write Project");
    expect(screen.queryByText("Read Project")).not.toBeInTheDocument();
  });
});
