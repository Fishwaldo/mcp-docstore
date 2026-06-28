import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import DiffView from "@/routes/DiffView";

vi.mock("@/lib/api", () => ({
  diffVersions: vi.fn().mockResolvedValue({
    diff: `--- a/doc\t2024-01-01\n+++ b/doc\t2024-06-01\n@@ -1,3 +1,3 @@\n line one\n-old line\n+new line\n line three\n`,
  }),
}));

// react-diff-view imports CSS; stub it out in tests
vi.mock("react-diff-view/style/index.css", () => ({}));

function wrapper({ children }: { children: React.ReactNode }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return (
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={["/documents/d1/diff?from=1&to=3"]}>
        <Routes>
          <Route path="/documents/:id/diff" element={children} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>
  );
}

describe("DiffView", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders version header", async () => {
    render(<DiffView />, { wrapper });
    await waitFor(() => {
      expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("version 1");
      expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("3");
    });
  });

  it("renders diff content", async () => {
    render(<DiffView />, { wrapper });
    await waitFor(() => {
      // react-diff-view renders changed lines in the DOM
      expect(screen.getByText("old line")).toBeInTheDocument();
      expect(screen.getByText("new line")).toBeInTheDocument();
    });
  });
});
