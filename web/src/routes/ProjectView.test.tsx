import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import ProjectView from "@/routes/ProjectView";
import { getProject, listDocuments, updateProject, archiveProject } from "@/lib/api";

vi.mock("@/lib/api", async () => {
  const actual = await vi.importActual<typeof import("@/lib/api")>("@/lib/api");
  return {
    ...actual,
    getProject: vi.fn(),
    listDocuments: vi.fn(),
    updateProject: vi.fn(),
    archiveProject: vi.fn(),
    unarchiveProject: vi.fn(),
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

    // documents load after the project resolves (query gated on !!project)
    await waitFor(() => {
      expect(screen.getByText("Doc One")).toBeInTheDocument();
    });
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

  it("does not fetch documents when the project 404s", async () => {
    vi.mocked(getProject).mockRejectedValue(new Error("API error 404: not found"));
    vi.mocked(listDocuments).mockResolvedValue([]);

    render(<ProjectView />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText(/not found/i)).toBeInTheDocument();
    });
    expect(listDocuments).not.toHaveBeenCalled();
  });

  it("shows an error when the document list fails to load", async () => {
    vi.mocked(getProject).mockResolvedValue({
      id: "p1",
      name: "Alpha Project",
      description: "",
      visibility: "org",
      archived: false,
      access: "read",
    });
    vi.mocked(listDocuments).mockRejectedValue(new Error("API error 500: boom"));

    render(<ProjectView />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText(/couldn.t load documents/i)).toBeInTheDocument();
    });
  });

  it("shows management controls only when can_manage is true", async () => {
    vi.mocked(getProject).mockResolvedValue({
      id: "p1",
      name: "Alpha Project",
      description: "desc",
      visibility: "org",
      archived: false,
      access: "write",
      can_manage: true,
    });
    vi.mocked(listDocuments).mockResolvedValue([]);

    render(<ProjectView />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("Alpha Project")).toBeInTheDocument();
    });
    expect(screen.getByRole("button", { name: "Edit" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Archive" })).toBeInTheDocument();
  });

  it("hides management controls for read-only users", async () => {
    vi.mocked(getProject).mockResolvedValue({
      id: "p1",
      name: "Alpha Project",
      description: "desc",
      visibility: "org",
      archived: false,
      access: "read",
      can_manage: false,
    });
    vi.mocked(listDocuments).mockResolvedValue([]);

    render(<ProjectView />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("Alpha Project")).toBeInTheDocument();
    });
    expect(screen.queryByRole("button", { name: "Edit" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Archive" })).not.toBeInTheDocument();
  });

  it("Edit → Save calls updateProject with the new name", async () => {
    vi.mocked(getProject).mockResolvedValue({
      id: "p1",
      name: "Alpha Project",
      description: "desc",
      visibility: "org",
      archived: false,
      access: "write",
      can_manage: true,
    });
    vi.mocked(listDocuments).mockResolvedValue([]);
    vi.mocked(updateProject).mockResolvedValue({
      id: "p1",
      name: "Beta Project",
      description: "desc",
      visibility: "org",
      archived: false,
      access: "write",
      can_manage: true,
    });

    render(<ProjectView />, { wrapper });

    await waitFor(() => {
      expect(screen.getByRole("button", { name: "Edit" })).toBeInTheDocument();
    });
    fireEvent.click(screen.getByRole("button", { name: "Edit" }));

    const nameInput = screen.getByRole("textbox", { name: /name/i });
    fireEvent.change(nameInput, { target: { value: "Beta Project" } });

    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => {
      expect(updateProject).toHaveBeenCalledWith("p1", {
        name: "Beta Project",
        description: "desc",
      });
    });
  });

  it("Archive calls archiveProject", async () => {
    vi.mocked(getProject).mockResolvedValue({
      id: "p1",
      name: "Alpha Project",
      description: "desc",
      visibility: "org",
      archived: false,
      access: "write",
      can_manage: true,
    });
    vi.mocked(listDocuments).mockResolvedValue([]);
    vi.mocked(archiveProject).mockResolvedValue({
      id: "p1",
      name: "Alpha Project",
      description: "desc",
      visibility: "org",
      archived: true,
      access: "write",
      can_manage: true,
    });

    render(<ProjectView />, { wrapper });

    await waitFor(() => {
      expect(screen.getByRole("button", { name: "Archive" })).toBeInTheDocument();
    });
    fireEvent.click(screen.getByRole("button", { name: "Archive" }));

    await waitFor(() => {
      expect(archiveProject).toHaveBeenCalledWith("p1");
    });
  });
});
