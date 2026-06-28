import { describe, it, expect, vi, afterEach, beforeEach } from "vitest";
import { listProjects, getProject, listDocuments, getDocument, searchDocuments } from "./api";

const mockFetch = vi.fn();

beforeEach(() => {
  vi.stubGlobal("fetch", mockFetch);
  Object.defineProperty(document, "cookie", {
    writable: true,
    configurable: true,
    value: "ds_csrf=test-csrf-token",
  });
});

afterEach(() => {
  vi.unstubAllGlobals();
  mockFetch.mockReset();
});

function makeResponse(status: number, body: unknown) {
  return {
    status,
    ok: status >= 200 && status < 300,
    json: async () => body,
    text: async () => String(body),
    statusText: "OK",
  };
}

describe("listProjects", () => {
  it("returns parsed projects on 200", async () => {
    const projects = [
      { id: "abc", name: "Test", description: "", visibility: "org", archived: false },
    ];
    mockFetch.mockResolvedValueOnce(makeResponse(200, projects));

    const result = await listProjects();
    expect(result).toEqual(projects);
    expect(mockFetch).toHaveBeenCalledWith(
      "/api/projects",
      expect.objectContaining({ credentials: "include" })
    );
  });

  it("appends include_archived param when true", async () => {
    mockFetch.mockResolvedValueOnce(makeResponse(200, []));
    await listProjects(true);
    expect(mockFetch).toHaveBeenCalledWith(
      "/api/projects?include_archived=true",
      expect.anything()
    );
  });

  it("throws on non-401 error", async () => {
    mockFetch.mockResolvedValueOnce({
      status: 500,
      ok: false,
      text: async () => "Internal Server Error",
      statusText: "Internal Server Error",
    });
    await expect(listProjects()).rejects.toThrow("API error 500");
  });
});

describe("getProject", () => {
  it("fetches a single project by id", async () => {
    const project = { id: "xyz", name: "X", description: "", visibility: "private", archived: false };
    mockFetch.mockResolvedValueOnce(makeResponse(200, project));

    const result = await getProject("xyz");
    expect(result).toEqual(project);
    expect(mockFetch).toHaveBeenCalledWith("/api/projects/xyz", expect.anything());
  });
});

describe("listDocuments", () => {
  it("fetches documents for a project", async () => {
    const docs = [{ id: "d1", title: "Doc", overview: "", tags: [], version: 1, updated_at: "" }];
    mockFetch.mockResolvedValueOnce(makeResponse(200, docs));

    const result = await listDocuments("proj-1");
    expect(result).toEqual(docs);
    expect(mockFetch).toHaveBeenCalledWith("/api/projects/proj-1/documents", expect.anything());
  });
});

describe("getDocument", () => {
  it("fetches a document by id", async () => {
    const doc = { id: "d1", title: "Doc", overview: "", tags: [], version: 1, updated_at: "", body_html: "<p>hi</p>", change_comment: "" };
    mockFetch.mockResolvedValueOnce(makeResponse(200, doc));

    const result = await getDocument("d1");
    expect(result).toEqual(doc);
    expect(mockFetch).toHaveBeenCalledWith("/api/documents/d1", expect.anything());
  });
});

describe("searchDocuments", () => {
  it("builds correct query string", async () => {
    mockFetch.mockResolvedValueOnce(makeResponse(200, []));
    await searchDocuments({ q: "hello", projectId: "p1", limit: 10 });
    expect(mockFetch).toHaveBeenCalledWith(
      "/api/search?q=hello&project_id=p1&limit=10",
      expect.anything()
    );
  });

  it("appends multiple tags", async () => {
    mockFetch.mockResolvedValueOnce(makeResponse(200, []));
    await searchDocuments({ q: "test", tags: ["a", "b"] });
    const url = mockFetch.mock.calls[0][0] as string;
    expect(url).toContain("tags=a");
    expect(url).toContain("tags=b");
  });
});

describe("401 redirect", () => {
  it("redirects to /auth/login on 401", async () => {
    mockFetch.mockResolvedValueOnce({
      status: 401,
      ok: false,
    });

    let assignedHref = "";
    Object.defineProperty(window, "location", {
      configurable: true,
      value: {
        ...window.location,
        set href(v: string) {
          assignedHref = v;
        },
      },
    });

    await expect(listProjects()).rejects.toThrow("Unauthenticated");
    expect(assignedHref).toBe("/auth/login");
  });
});
