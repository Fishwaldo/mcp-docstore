import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import ProjectTree from "@/components/ProjectTree";
import { listProjects, searchDocuments } from "@/lib/api";

const { ACTIVE_PROJECT, ARCHIVED_PROJECT } = vi.hoisted(() => ({
  ACTIVE_PROJECT: {
    id: "p1",
    name: "Alpha Project",
    description: "",
    visibility: "org",
    archived: false,
  },
  ARCHIVED_PROJECT: {
    id: "p2",
    name: "Old Project",
    description: "",
    visibility: "org",
    archived: true,
  },
}));

vi.mock("@/lib/api", () => ({
  listProjects: vi.fn((includeArchived?: boolean) =>
    Promise.resolve(
      includeArchived ? [ACTIVE_PROJECT, ARCHIVED_PROJECT] : [ACTIVE_PROJECT],
    ),
  ),
  listDocuments: vi.fn().mockResolvedValue([
    { id: "b", title: "Zeta", overview: "", tags: [], version: 1, updated_at: "2026-02-01T00:00:00Z" },
    { id: "a", title: "Alpha", overview: "", tags: [], version: 1, updated_at: "2026-01-01T00:00:00Z" },
  ]),
  listTags: vi.fn().mockResolvedValue(["alpha", "beta"]),
  searchDocuments: vi.fn().mockResolvedValue([
    { document_id: "d1", project_id: "p1", title: "Matching Doc", overview: "", score: 1, snippet: "" },
  ]),
}));

function wrapper({ children }: { children: React.ReactNode }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return (
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={["/"]}>
        <Routes>
          <Route path="/" element={children} />
          <Route path="/projects/:id" element={<div>Project page</div>} />
        </Routes>
      </MemoryRouter>
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
    fireEvent.click(screen.getByLabelText(/toggle alpha project/i));

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

    fireEvent.click(screen.getByRole("radio", { name: "Recent" }));

    await waitFor(() => {
      const titles = docLinkTitles();
      expect(titles.indexOf("Zeta")).toBeLessThan(titles.indexOf("Alpha"));
    });

    expect(localStorage.getItem("docOrder")).toBe("recent");
  });
});

describe("ProjectTree name/chevron interaction", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
  });

  it("clicking the project name navigates to /projects/:id without expanding", async () => {
    render(<ProjectTree />, { wrapper });

    await waitFor(() => {
      expect(screen.getByRole("link", { name: /Alpha Project/ })).toBeInTheDocument();
    });
    fireEvent.click(screen.getByRole("link", { name: /Alpha Project/ }));

    await waitFor(() => {
      expect(screen.getByText("Project page")).toBeInTheDocument();
    });
    expect(screen.queryByText("Zeta")).not.toBeInTheDocument();
  });

  it("clicking the chevron expands and shows lazy-loaded docs without navigating", async () => {
    render(<ProjectTree />, { wrapper });

    await waitFor(() => {
      expect(screen.getByLabelText(/toggle alpha project/i)).toBeInTheDocument();
    });
    fireEvent.click(screen.getByLabelText(/toggle alpha project/i));

    await waitFor(() => {
      expect(screen.getByText("Zeta")).toBeInTheDocument();
      expect(screen.getByText("Alpha")).toBeInTheDocument();
    });
    expect(screen.queryByText("Project page")).not.toBeInTheDocument();
  });
});

describe("ProjectTree tag filter", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
  });

  it("narrows the tree to matching projects/docs when a tag is checked, and restores on uncheck", async () => {
    render(<ProjectTree />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("Alpha Project")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByRole("button", { name: /filter by tag/i }));

    await waitFor(() => {
      expect(screen.getByRole("checkbox", { name: "alpha" })).toBeInTheDocument();
    });

    fireEvent.click(screen.getByRole("checkbox", { name: "alpha" }));

    await waitFor(() => {
      expect(searchDocuments).toHaveBeenCalledWith({ q: "", tags: ["alpha"] });
    });

    await waitFor(() => {
      expect(screen.getByRole("link", { name: "Matching Doc" })).toHaveAttribute(
        "href",
        "/documents/d1",
      );
    });
    expect(screen.getByText("Alpha Project")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("checkbox", { name: "alpha" }));

    await waitFor(() => {
      expect(screen.queryByRole("link", { name: "Matching Doc" })).not.toBeInTheDocument();
    });
    expect(screen.getByText("Alpha Project")).toBeInTheDocument();
  });
});

describe("ProjectTree show archived toggle", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
  });

  it("hides archived projects by default and reveals them (dimmed) when toggled on", async () => {
    render(<ProjectTree />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("Alpha Project")).toBeInTheDocument();
    });
    expect(listProjects).toHaveBeenCalledWith(false);
    expect(screen.queryByText("Old Project")).not.toBeInTheDocument();

    const toggle = screen.getByRole("button", { name: /show archived/i });
    expect(toggle).toHaveAttribute("aria-pressed", "false");

    fireEvent.click(toggle);

    await waitFor(() => {
      expect(listProjects).toHaveBeenCalledWith(true);
      expect(screen.getByText("Old Project")).toBeInTheDocument();
    });
    expect(toggle).toHaveAttribute("aria-pressed", "true");

    const archivedLink = screen.getByRole("link", { name: /Old Project/ });
    const archivedRow = archivedLink.closest("[class*='opacity-60']");
    expect(archivedRow).not.toBeNull();
  });
});
