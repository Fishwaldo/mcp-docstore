import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import AppShell from "@/components/AppShell";

vi.mock("@/lib/api", () => ({
  listProjects: vi.fn().mockResolvedValue([
    { id: "p1", name: "Alpha Project", description: "", visibility: "org", archived: false },
    { id: "p2", name: "Beta Project", description: "", visibility: "private", archived: false },
  ]),
  listDocuments: vi.fn().mockResolvedValue([]),
}));

function wrapper({ children }: { children: React.ReactNode }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return (
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={["/"]}>{children}</MemoryRouter>
    </QueryClientProvider>
  );
}

describe("AppShell", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders project names in the sidebar", async () => {
    render(<AppShell />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("Alpha Project")).toBeInTheDocument();
      expect(screen.getByText("Beta Project")).toBeInTheDocument();
    });
  });

  it("renders the DocStore title", () => {
    render(<AppShell />, { wrapper });
    expect(screen.getByText("DocStore")).toBeInTheDocument();
  });
});
