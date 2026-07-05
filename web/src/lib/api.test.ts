import { describe, it, expect, vi, afterEach, beforeEach } from "vitest";

const getAccessToken = vi.fn();
const refreshAccessToken = vi.fn();
const login = vi.fn();

vi.mock("./oauth", () => ({
  getAccessToken: (...args: unknown[]) => getAccessToken(...args),
  refreshAccessToken: (...args: unknown[]) => refreshAccessToken(...args),
  login: (...args: unknown[]) => login(...args),
}));

import {
  listProjects,
  getProject,
  listDocuments,
  getDocument,
  searchDocuments,
  getMe,
  editDocument,
  createDocument,
  deleteDocument,
  restoreSnapshot,
  listTags,
  createProject,
  updateProject,
  archiveProject,
  unarchiveProject,
  deleteProject,
  listShares,
  addShares,
  removeShares,
  ApiNoAccessError,
  ConflictError,
  NO_ACCESS_EVENT,
} from "./api";

const mockFetch = vi.fn();

beforeEach(() => {
  vi.stubGlobal("fetch", mockFetch);
  getAccessToken.mockReset().mockResolvedValue("token-1");
  refreshAccessToken.mockReset().mockResolvedValue("token-2");
  login.mockReset().mockResolvedValue(undefined);
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
    text: async () => JSON.stringify(body),
    statusText: "OK",
  };
}

function authHeader(call: unknown[]): string | null {
  const init = call[1] as RequestInit;
  return new Headers(init.headers).get("Authorization");
}

describe("listProjects", () => {
  it("returns parsed projects on 200 and attaches the bearer token", async () => {
    const projects = [
      { id: "abc", name: "Test", description: "", visibility: "org", archived: false },
    ];
    mockFetch.mockResolvedValueOnce(makeResponse(200, projects));

    const result = await listProjects();
    expect(result).toEqual(projects);
    expect(mockFetch).toHaveBeenCalledWith("/api/projects", expect.anything());
    expect(authHeader(mockFetch.mock.calls[0])).toBe("Bearer token-1");
  });

  it("appends include_archived param when true", async () => {
    mockFetch.mockResolvedValueOnce(makeResponse(200, []));
    await listProjects(true);
    expect(mockFetch).toHaveBeenCalledWith(
      "/api/projects?include_archived=true",
      expect.anything()
    );
  });

  it("throws on non-401/403 error", async () => {
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

describe("getMe", () => {
  it("fetches the caller's identity", async () => {
    const me = { email: "a@example.com", tenant: "acme", groups: ["eng"] };
    mockFetch.mockResolvedValueOnce(makeResponse(200, me));

    const result = await getMe();
    expect(result).toEqual(me);
    expect(mockFetch).toHaveBeenCalledWith("/api/me", expect.anything());
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

describe("401 handling", () => {
  it("forces a refresh and retries exactly once on a single 401", async () => {
    mockFetch
      .mockResolvedValueOnce(makeResponse(401, { error: "invalid_token" }))
      .mockResolvedValueOnce(makeResponse(200, []));

    const result = await listProjects();

    expect(result).toEqual([]);
    expect(mockFetch).toHaveBeenCalledTimes(2);
    expect(refreshAccessToken).toHaveBeenCalledTimes(1);
    expect(authHeader(mockFetch.mock.calls[0])).toBe("Bearer token-1");
    expect(authHeader(mockFetch.mock.calls[1])).toBe("Bearer token-2");
    expect(login).not.toHaveBeenCalled();
  });

  it("redirects to login when the retried request also gets a 401", async () => {
    mockFetch
      .mockResolvedValueOnce(makeResponse(401, { error: "invalid_token" }))
      .mockResolvedValueOnce(makeResponse(401, { error: "invalid_token" }));

    await expect(listProjects()).rejects.toThrow("Unauthenticated");

    expect(mockFetch).toHaveBeenCalledTimes(2);
    expect(login).toHaveBeenCalledTimes(1);
  });
});

describe("editDocument", () => {
  it("PATCHes and returns the updated doc", async () => {
    mockFetch.mockResolvedValueOnce(
      makeResponse(200, {
        id: "d1",
        title: "T",
        body: "# T",
        body_html: "<h1>T</h1>",
        overview: "",
        tags: [],
        version: 2,
        change_comment: "",
        updated_at: "2026-01-01T00:00:00Z",
      })
    );
    const doc = await editDocument("d1", { base_version: 1, overview: "", body: "# T", tags: [] });
    expect(doc.version).toBe(2);
    const [url, opts] = mockFetch.mock.calls[0];
    expect(url).toBe("/api/documents/d1");
    expect((opts as RequestInit).method).toBe("PATCH");
    expect(authHeader(mockFetch.mock.calls[0])).toBe("Bearer token-1");
  });

  it("throws ConflictError with the current version on 409", async () => {
    mockFetch.mockResolvedValueOnce(
      makeResponse(409, { title: "Conflict", status: 409, detail: "version conflict: current version is 5" })
    );
    const err = await editDocument("d1", { base_version: 1, overview: "", body: "x", tags: [] }).catch(
      (e: unknown) => e
    );
    expect(err).toBeInstanceOf(ConflictError);
    expect(err).toMatchObject({ name: "ConflictError", currentVersion: 5 });
  });
});

describe("createDocument", () => {
  it("POSTs and returns the created doc", async () => {
    mockFetch.mockResolvedValueOnce(
      makeResponse(201, {
        id: "d2",
        title: "New",
        body: "# New",
        body_html: "<h1>New</h1>",
        overview: "",
        tags: [],
        version: 1,
        change_comment: "",
        updated_at: "2026-01-01T00:00:00Z",
      })
    );
    const doc = await createDocument({ project_id: "p1", title: "New" });
    expect(doc.id).toBe("d2");
    const [url, opts] = mockFetch.mock.calls[0];
    expect(url).toBe("/api/documents");
    expect((opts as RequestInit).method).toBe("POST");
  });
});

describe("deleteDocument", () => {
  it("DELETEs and resolves void", async () => {
    mockFetch.mockResolvedValueOnce(makeResponse(204, null));
    await expect(deleteDocument("d1")).resolves.toBeUndefined();
    expect((mockFetch.mock.calls[0][1] as RequestInit).method).toBe("DELETE");
  });
});

describe("restoreSnapshot", () => {
  it("POSTs to the restore endpoint and returns the restored doc", async () => {
    mockFetch.mockResolvedValueOnce(
      makeResponse(200, {
        id: "d1",
        title: "T",
        body: "# old",
        body_html: "<h1>old</h1>",
        overview: "",
        tags: [],
        version: 3,
        change_comment: "",
        updated_at: "2026-01-01T00:00:00Z",
      })
    );
    const doc = await restoreSnapshot("d1", { version: 1, base_version: 2 });
    expect(doc.version).toBe(3);
    const [url, opts] = mockFetch.mock.calls[0];
    expect(url).toBe("/api/documents/d1/restore");
    expect((opts as RequestInit).method).toBe("POST");
  });
});

describe("listTags", () => {
  it("unwraps the tags array", async () => {
    mockFetch.mockResolvedValueOnce(makeResponse(200, { tags: ["alpha", "beta"] }));
    expect(await listTags()).toEqual(["alpha", "beta"]);
  });
});

describe("createProject", () => {
  it("POSTs and returns the project", async () => {
    mockFetch.mockResolvedValueOnce(
      makeResponse(201, {
        id: "p1",
        name: "New",
        description: "",
        visibility: "private",
        archived: false,
        access: "write",
        can_manage: true,
      })
    );
    const p = await createProject({ name: "New", visibility: "private" });
    expect(p.id).toBe("p1");
    const [url, opts] = mockFetch.mock.calls[0];
    expect(url).toBe("/api/projects");
    expect((opts as RequestInit).method).toBe("POST");
  });
});

describe("updateProject", () => {
  it("PATCHes", async () => {
    mockFetch.mockResolvedValueOnce(
      makeResponse(200, {
        id: "p1",
        name: "Renamed",
        description: "",
        visibility: "org",
        archived: false,
        can_manage: true,
      })
    );
    const p = await updateProject("p1", { name: "Renamed" });
    expect(p.name).toBe("Renamed");
    expect((mockFetch.mock.calls[0][1] as RequestInit).method).toBe("PATCH");
  });
});

describe("archiveProject", () => {
  it("POSTs to /archive", async () => {
    mockFetch.mockResolvedValueOnce(
      makeResponse(200, { id: "p1", name: "N", description: "", visibility: "org", archived: true, can_manage: true })
    );
    const p = await archiveProject("p1");
    expect(p.archived).toBe(true);
    expect(mockFetch.mock.calls[0][0]).toBe("/api/projects/p1/archive");
  });
});

describe("unarchiveProject", () => {
  it("POSTs to /unarchive", async () => {
    mockFetch.mockResolvedValueOnce(
      makeResponse(200, { id: "p1", name: "N", description: "", visibility: "org", archived: false, can_manage: true })
    );
    const p = await unarchiveProject("p1");
    expect(p.archived).toBe(false);
    expect(mockFetch.mock.calls[0][0]).toBe("/api/projects/p1/unarchive");
    expect((mockFetch.mock.calls[0][1] as RequestInit).method).toBe("POST");
  });
});

describe("deleteProject", () => {
  it("DELETEs and resolves void", async () => {
    mockFetch.mockResolvedValueOnce(makeResponse(204, null));
    await expect(deleteProject("p1")).resolves.toBeUndefined();
    expect((mockFetch.mock.calls[0][1] as RequestInit).method).toBe("DELETE");
  });
});

describe("listShares", () => {
  it("listShares GETs the shares", async () => {
    mockFetch.mockResolvedValueOnce(
      makeResponse(200, { users: [{ email: "a@x.com", permission: "read" }], groups: [] })
    );
    const s = await listShares("p1");
    expect(s.users[0].email).toBe("a@x.com");
    expect(mockFetch.mock.calls[0][0]).toBe("/api/projects/p1/shares");
  });
});

describe("addShares", () => {
  it("addShares POSTs kind/principals/permission and returns shares+unresolved", async () => {
    mockFetch.mockResolvedValueOnce(
      makeResponse(200, { shares: { users: [], groups: [] }, unresolved: ["nobody@x.com"] })
    );
    const out = await addShares("p1", { kind: "user", principals: ["nobody@x.com"], permission: "write" });
    expect(out.unresolved).toContain("nobody@x.com");
    const [url, opts] = mockFetch.mock.calls[0];
    expect(url).toBe("/api/projects/p1/shares");
    expect((opts as RequestInit).method).toBe("POST");
  });
});

describe("removeShares", () => {
  it("removeShares DELETEs with a body and resolves void", async () => {
    mockFetch.mockResolvedValueOnce(makeResponse(204, null));
    await expect(removeShares("p1", { kind: "user", principals: ["a@x.com"] })).resolves.toBeUndefined();
    expect((mockFetch.mock.calls[0][1] as RequestInit).method).toBe("DELETE");
  });
});

describe("403 no_access handling", () => {
  it("throws ApiNoAccessError and dispatches the no-access event without redirecting to login", async () => {
    mockFetch.mockResolvedValueOnce(
      makeResponse(403, { error: "no_access", error_description: "authenticated but not authorized for any tenant" })
    );

    const listener = vi.fn();
    window.addEventListener(NO_ACCESS_EVENT, listener);

    await expect(listProjects()).rejects.toBeInstanceOf(ApiNoAccessError);

    expect(listener).toHaveBeenCalledTimes(1);
    expect(login).not.toHaveBeenCalled();
    expect(refreshAccessToken).not.toHaveBeenCalled();

    window.removeEventListener(NO_ACCESS_EVENT, listener);
  });
});
