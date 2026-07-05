import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import SharesPanel from "@/components/SharesPanel";
import { listShares, removeShares, addShares } from "@/lib/api";

vi.mock("@/lib/api", async () => {
  const actual = await vi.importActual<typeof import("@/lib/api")>("@/lib/api");
  return {
    ...actual,
    listShares: vi.fn(),
    removeShares: vi.fn(),
    addShares: vi.fn(),
  };
});

function wrapper({ children }: { children: React.ReactNode }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

describe("SharesPanel", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders user email, group name, and permission badges", async () => {
    vi.mocked(listShares).mockResolvedValue({
      users: [{ email: "a@x.com", permission: "read" }],
      groups: [{ group: "engineers", permission: "write" }],
    });

    render(<SharesPanel projectId="p1" />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("a@x.com")).toBeInTheDocument();
    });
    expect(screen.getByText("engineers")).toBeInTheDocument();
    expect(screen.getByText("read")).toBeInTheDocument();
    expect(screen.getByText("write")).toBeInTheDocument();
  });

  it("clicking a user's remove button calls removeShares", async () => {
    vi.mocked(listShares).mockResolvedValue({
      users: [{ email: "a@x.com", permission: "read" }],
      groups: [],
    });
    vi.mocked(removeShares).mockResolvedValue(undefined);

    render(<SharesPanel projectId="p1" />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("a@x.com")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByRole("button", { name: /remove a@x.com/i }));

    await waitFor(() => {
      expect(removeShares).toHaveBeenCalledWith("p1", {
        kind: "user",
        principals: ["a@x.com"],
      });
    });
  });

  it("clicking a group's remove button calls removeShares", async () => {
    vi.mocked(listShares).mockResolvedValue({
      users: [],
      groups: [{ group: "engineers", permission: "write" }],
    });
    vi.mocked(removeShares).mockResolvedValue(undefined);

    render(<SharesPanel projectId="p1" />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("engineers")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByRole("button", { name: /remove engineers/i }));

    await waitFor(() => {
      expect(removeShares).toHaveBeenCalledWith("p1", {
        kind: "group",
        principals: ["engineers"],
      });
    });
  });

  it("shows a loading state", () => {
    vi.mocked(listShares).mockReturnValue(new Promise(() => {}));

    render(<SharesPanel projectId="p1" />, { wrapper });

    expect(screen.getByText(/loading shares/i)).toBeInTheDocument();
  });

  it("shows an error state", async () => {
    vi.mocked(listShares).mockRejectedValue(new Error("boom"));

    render(<SharesPanel projectId="p1" />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText(/couldn.t load shares/i)).toBeInTheDocument();
    });
  });

  it("shows an empty state when there are no shares", async () => {
    vi.mocked(listShares).mockResolvedValue({ users: [], groups: [] });

    render(<SharesPanel projectId="p1" />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText(/no shares yet/i)).toBeInTheDocument();
    });
  });

  it("adding a share with write permission calls addShares", async () => {
    vi.mocked(listShares).mockResolvedValue({ users: [], groups: [] });
    vi.mocked(addShares).mockResolvedValue({
      shares: { users: [{ email: "b@x.com", permission: "write" }], groups: [] },
      unresolved: [],
    });

    render(<SharesPanel projectId="p1" />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText(/no shares yet/i)).toBeInTheDocument();
    });

    fireEvent.change(screen.getByPlaceholderText(/email or group name/i), {
      target: { value: "b@x.com" },
    });
    fireEvent.click(screen.getByRole("radio", { name: /write/i }));
    fireEvent.click(screen.getByRole("button", { name: /^add$/i }));

    await waitFor(() => {
      expect(addShares).toHaveBeenCalledWith("p1", {
        kind: "user",
        principals: ["b@x.com"],
        permission: "write",
      });
    });
  });

  it("shows a warning naming unresolved principals after add", async () => {
    vi.mocked(listShares).mockResolvedValue({ users: [], groups: [] });
    vi.mocked(addShares).mockResolvedValue({
      shares: { users: [], groups: [] },
      unresolved: ["b@x.com"],
    });

    render(<SharesPanel projectId="p1" />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText(/no shares yet/i)).toBeInTheDocument();
    });

    fireEvent.change(screen.getByPlaceholderText(/email or group name/i), {
      target: { value: "b@x.com" },
    });
    fireEvent.click(screen.getByRole("button", { name: /^add$/i }));

    await waitFor(() => {
      expect(screen.getByText(/couldn.t resolve.*b@x\.com/i)).toBeInTheDocument();
    });
  });
});
