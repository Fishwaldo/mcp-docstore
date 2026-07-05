import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import NewProjectDialog from "@/components/NewProjectDialog";
import { createProject } from "@/lib/api";

vi.mock("@/lib/api", () => ({
  createProject: vi.fn(),
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

describe("NewProjectDialog", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("Create is disabled until a name is entered", async () => {
    render(<NewProjectDialog />, { wrapper });

    fireEvent.click(screen.getByRole("button", { name: /new project/i }));

    const createButton = await screen.findByRole("button", { name: "Create" });
    expect(createButton).toBeDisabled();

    fireEvent.change(screen.getByLabelText(/name/i), { target: { value: "My Project" } });
    expect(createButton).not.toBeDisabled();
  });

  it("creates a private project and navigates to it", async () => {
    vi.mocked(createProject).mockResolvedValue({
      id: "new1",
      name: "My Project",
      description: "desc",
      visibility: "private",
      archived: false,
    });

    render(<NewProjectDialog />, { wrapper });

    fireEvent.click(screen.getByRole("button", { name: /new project/i }));

    fireEvent.change(await screen.findByLabelText(/name/i), {
      target: { value: "My Project" },
    });
    fireEvent.click(screen.getByRole("radio", { name: "Private" }));
    fireEvent.change(screen.getByLabelText(/description/i), { target: { value: "desc" } });

    fireEvent.click(screen.getByRole("button", { name: "Create" }));

    await waitFor(() => {
      expect(createProject).toHaveBeenCalledWith({
        name: "My Project",
        visibility: "private",
        description: "desc",
      });
    });

    await waitFor(() => {
      expect(screen.getByText("Project page")).toBeInTheDocument();
    });
  });
});
