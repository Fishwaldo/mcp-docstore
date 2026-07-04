import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import DocumentView from "@/routes/DocumentView";
import {
  getDocument,
  listSnapshots,
  getProject,
  editDocument,
  deleteDocument,
  restoreSnapshot,
  listTags,
  ConflictError,
} from "@/lib/api";

vi.mock("@/components/MarkdownEditor", () => ({
  default: ({ markdown, onChange }: { markdown: string; onChange: (v: string) => void }) => (
    <textarea data-testid="md-editor" value={markdown} onChange={(e) => onChange(e.target.value)} />
  ),
}));

vi.mock("@/lib/api", async () => {
  const actual = await vi.importActual<typeof import("@/lib/api")>("@/lib/api");
  return {
    ConflictError: actual.ConflictError,
    getDocument: vi.fn(),
    listSnapshots: vi.fn(),
    getProject: vi.fn(),
    editDocument: vi.fn(),
    deleteDocument: vi.fn(),
    restoreSnapshot: vi.fn(),
    listTags: vi.fn(),
  };
});

const baseDoc = {
  id: "d1",
  project_id: "p1",
  title: "Test Document",
  overview: "A test overview",
  body: "# Hello\n\nHello world",
  tags: ["foo", "bar"],
  version: 3,
  change_comment: "updated",
  updated_at: "2024-06-01T00:00:00Z",
  body_html: "<p>Hello world</p>",
};

function wrapper({ children }: { children: React.ReactNode }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return (
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={["/documents/d1"]}>
        <Routes>
          <Route path="/documents/:id" element={children} />
          <Route path="/documents/:id/diff" element={<div>Diff page</div>} />
          <Route path="/projects/:id" element={<div>Project page</div>} />
          <Route path="/" element={<div>Home page</div>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>
  );
}

describe("DocumentView", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(getDocument).mockResolvedValue(baseDoc);
    vi.mocked(listSnapshots).mockResolvedValue([
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
    ]);
    vi.mocked(listTags).mockResolvedValue([]);
    vi.mocked(getProject).mockResolvedValue({
      id: "p1",
      name: "Project",
      description: "",
      visibility: "org",
      archived: false,
      access: "write",
    });
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

  it("shows an Edit button only when the project grants write access", async () => {
    render(<DocumentView />, { wrapper });
    await waitFor(() => {
      expect(screen.getByRole("button", { name: "Edit" })).toBeInTheDocument();
    });
  });

  it("hides the Edit button for read-only access", async () => {
    vi.mocked(getProject).mockResolvedValue({
      id: "p1",
      name: "Project",
      description: "",
      visibility: "org",
      archived: false,
      access: "read",
    });
    render(<DocumentView />, { wrapper });
    await waitFor(() => {
      expect(screen.getByText("Test Document")).toBeInTheDocument();
    });
    await waitFor(() => {
      expect(vi.mocked(getProject)).toHaveBeenCalled();
    });
    expect(screen.queryByRole("button", { name: "Edit" })).not.toBeInTheDocument();
  });

  it("entering edit shows the markdown editor seeded with the raw body", async () => {
    render(<DocumentView />, { wrapper });
    const editButton = await screen.findByRole("button", { name: "Edit" });
    fireEvent.click(editButton);
    await waitFor(() => {
      expect(screen.getByTestId("md-editor")).toHaveValue(baseDoc.body);
    });
  });

  it("Save with no changes returns to view without calling editDocument", async () => {
    render(<DocumentView />, { wrapper });
    const editButton = await screen.findByRole("button", { name: "Edit" });
    fireEvent.click(editButton);
    await screen.findByTestId("md-editor");

    const saveButton = screen.getByRole("button", { name: "Save" });
    fireEvent.click(saveButton);

    await waitFor(() => {
      expect(screen.getByRole("button", { name: "Edit" })).toBeInTheDocument();
    });
    expect(editDocument).not.toHaveBeenCalled();
  });

  it("Save with an edit calls editDocument with base_version and applies", async () => {
    vi.mocked(editDocument).mockResolvedValue({ ...baseDoc, body: "changed body", version: 4 });

    render(<DocumentView />, { wrapper });
    const editButton = await screen.findByRole("button", { name: "Edit" });
    fireEvent.click(editButton);
    const editor = await screen.findByTestId("md-editor");

    fireEvent.change(editor, { target: { value: "changed body" } });
    const saveButton = screen.getByRole("button", { name: "Save" });
    fireEvent.click(saveButton);

    await waitFor(() => {
      expect(editDocument).toHaveBeenCalledWith("d1", {
        base_version: 3,
        overview: baseDoc.overview,
        body: "changed body",
        tags: baseDoc.tags,
      });
    });
    await waitFor(() => {
      expect(screen.getByRole("button", { name: "Edit" })).toBeInTheDocument();
    });
  });

  it("a 409 on save shows the conflict banner with Reload and Keep editing", async () => {
    vi.mocked(editDocument).mockRejectedValue(
      new ConflictError(5, "current version is 5")
    );

    render(<DocumentView />, { wrapper });
    const editButton = await screen.findByRole("button", { name: "Edit" });
    fireEvent.click(editButton);
    const editor = await screen.findByTestId("md-editor");

    fireEvent.change(editor, { target: { value: "changed body" } });
    const saveButton = screen.getByRole("button", { name: "Save" });
    fireEvent.click(saveButton);

    await waitFor(() => {
      expect(screen.getByText(/current version 5/i)).toBeInTheDocument();
    });
    expect(screen.getByRole("button", { name: "Reload" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Keep editing" })).toBeInTheDocument();
  });

  it("Delete asks for confirmation then calls deleteDocument", async () => {
    vi.mocked(deleteDocument).mockResolvedValue(undefined);

    render(<DocumentView />, { wrapper });
    const editButton = await screen.findByRole("button", { name: "Edit" });
    fireEvent.click(editButton);

    const deleteButton = await screen.findByRole("button", { name: "Delete" });
    fireEvent.click(deleteButton);

    // Confirmation dialog appears; deleteDocument should not fire yet.
    const confirmButton = await screen.findByRole("button", { name: "Yes, delete" });
    expect(deleteDocument).not.toHaveBeenCalled();
    fireEvent.click(confirmButton);

    await waitFor(() => {
      expect(deleteDocument).toHaveBeenCalledWith("d1");
    });
    await waitFor(() => {
      expect(screen.getByText("Project page")).toBeInTheDocument();
    });
  });

  it("shows an inline error and keeps the dialog usable when deleteDocument fails", async () => {
    vi.mocked(deleteDocument).mockRejectedValue(new Error("network error"));

    render(<DocumentView />, { wrapper });
    const editButton = await screen.findByRole("button", { name: "Edit" });
    fireEvent.click(editButton);

    const deleteButton = await screen.findByRole("button", { name: "Delete" });
    fireEvent.click(deleteButton);

    const confirmButton = await screen.findByRole("button", { name: "Yes, delete" });
    fireEvent.click(confirmButton);

    await waitFor(() => {
      expect(screen.getByText(/network error/i)).toBeInTheDocument();
    });
    // Dialog stays open and the confirm button is still usable (not stuck disabled).
    expect(screen.getByRole("button", { name: "Yes, delete" })).toBeEnabled();
    expect(screen.queryByText("Home page")).not.toBeInTheDocument();
  });

  it("shows an inline error and keeps the dialog usable when restoreSnapshot fails", async () => {
    vi.mocked(restoreSnapshot).mockRejectedValue(new Error("stale version"));

    render(<DocumentView />, { wrapper });
    await screen.findByText("Test Document");

    const restoreButtons = await screen.findAllByRole("button", { name: "Restore" });
    fireEvent.click(restoreButtons[0]);

    const confirmButton = await screen.findByRole("button", { name: "Yes, restore" });
    fireEvent.click(confirmButton);

    await waitFor(() => {
      expect(screen.getByText(/stale version/i)).toBeInTheDocument();
    });
    expect(screen.getByRole("button", { name: "Yes, restore" })).toBeEnabled();
  });

  it("Restore on a snapshot calls restoreSnapshot with base_version and body scope", async () => {
    vi.mocked(restoreSnapshot).mockResolvedValue({ ...baseDoc, version: 4 });

    render(<DocumentView />, { wrapper });
    await screen.findByText("Test Document");

    const restoreButtons = await screen.findAllByRole("button", { name: "Restore" });
    fireEvent.click(restoreButtons[0]);

    const confirmButton = await screen.findByRole("button", { name: "Yes, restore" });
    expect(restoreSnapshot).not.toHaveBeenCalled();
    fireEvent.click(confirmButton);

    await waitFor(() => {
      expect(restoreSnapshot).toHaveBeenCalledWith("d1", {
        version: 1,
        base_version: 3,
        scope: "body",
      });
    });
  });
});
