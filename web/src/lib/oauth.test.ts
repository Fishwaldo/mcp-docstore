import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

const ORIGIN = "http://localhost:3000";

// The exact RFC 7636 Appendix B test vector: this verifier's S256 challenge is a known,
// published value, so reproducing it end-to-end (random-bytes -> base64url -> SHA-256 ->
// base64url) is a strong proof the PKCE math is right, not just internally self-consistent.
const RFC7636_VERIFIER = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk";
const RFC7636_CHALLENGE = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM";
const RFC7636_VERIFIER_BYTES = new Uint8Array([
  116, 24, 223, 180, 151, 153, 224, 37, 79, 250, 96, 125, 216, 173, 187, 186, 22, 212, 37, 77,
  105, 214, 191, 240, 91, 88, 5, 88, 83, 132, 141, 121,
]);
void RFC7636_VERIFIER; // documents which verifier the byte array corresponds to

function jsonResponse(status: number, body: unknown): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
    text: async () => JSON.stringify(body),
  } as Response;
}

// setLocation replaces window.location wholesale with a plain object carrying a jest.fn() for
// assign (jsdom's real assign throws "not implemented"). Returns the assign spy for assertions.
function setLocation(init: { href?: string; pathname?: string; search?: string } = {}): ReturnType<typeof vi.fn> {
  const assign = vi.fn();
  Object.defineProperty(window, "location", {
    configurable: true,
    value: {
      origin: ORIGIN,
      href: init.href ?? `${ORIGIN}/`,
      pathname: init.pathname ?? "/",
      search: init.search ?? "",
      assign,
    },
  });
  return assign;
}

function stateFromAuthorizeUrl(assign: ReturnType<typeof vi.fn>, callIndex = 0): string {
  const url = new URL(assign.mock.calls[callIndex][0] as string);
  return url.searchParams.get("state") as string;
}

// oauth.ts holds module-scoped state (the in-memory access token, the single-flight refresh
// promise), so each test re-imports a fresh module instance rather than sharing one across
// cases.
let oauth: typeof import("./oauth");
let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(async () => {
  vi.resetModules();
  sessionStorage.clear();
  fetchMock = vi.fn();
  vi.stubGlobal("fetch", fetchMock);
  setLocation();
  oauth = await import("./oauth");
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

// performLogin runs oauth.login(returnTo) against a fresh setLocation() call and returns the
// state it generated, so a test can build a matching /auth/callback URL.
async function performLogin(returnTo: string): Promise<string> {
  const assign = setLocation();
  await oauth.login(returnTo);
  return stateFromAuthorizeUrl(assign);
}

// completeCallback seeds a successful authorization-code exchange: it drives login() then
// simulates landing back on /auth/callback with a matching code+state, resolving the token
// endpoint with the given token response body.
async function completeCallback(returnTo: string, tokenResponse: Record<string, unknown>) {
  const state = await performLogin(returnTo);
  fetchMock.mockResolvedValueOnce(jsonResponse(200, tokenResponse));
  const assign = setLocation({
    href: `${ORIGIN}/auth/callback?code=abc123&state=${state}`,
    pathname: "/auth/callback",
    search: `?code=abc123&state=${state}`,
  });
  await oauth.handleCallback();
  return assign;
}

describe("login", () => {
  it("produces the exact RFC 7636 S256 challenge for a known verifier", async () => {
    const getRandomValues = vi
      .spyOn(crypto, "getRandomValues")
      .mockImplementation((<T extends ArrayBufferView>(arr: T): T => {
        const bytes = arr as unknown as Uint8Array;
        if (bytes.length === 32) {
          bytes.set(RFC7636_VERIFIER_BYTES);
        } else {
          bytes.fill(7);
        }
        return arr;
      }) as typeof crypto.getRandomValues);

    const assign = setLocation();
    await oauth.login("/return/here");

    const url = new URL(assign.mock.calls[0][0] as string);
    const params = url.searchParams;
    expect(params.get("code_challenge_method")).toBe("S256");
    expect(params.get("code_challenge")).toBe(RFC7636_CHALLENGE);
    expect(params.get("client_id")).toBe("docstore-web");
    expect(params.get("redirect_uri")).toBe(`${ORIGIN}/auth/callback`);
    expect(params.get("response_type")).toBe("code");
    expect(params.get("scope")).toBe("openid profile email groups offline_access");
    expect(params.has("resource")).toBe(false);
    expect(url.origin + url.pathname).toBe(`${ORIGIN}/oauth/authorize`);

    getRandomValues.mockRestore();
  });

  it("stashes a random state and redirects to /oauth/authorize", async () => {
    const assign = setLocation();
    await oauth.login("/documents/abc");

    expect(assign).toHaveBeenCalledTimes(1);
    const state = stateFromAuthorizeUrl(assign);
    expect(state).toBeTruthy();
    expect(state.length).toBeGreaterThan(10);
  });
});

describe("handleCallback", () => {
  it("round-trips state and navigates to returnTo after a successful exchange", async () => {
    const assign = await completeCallback("/documents/xyz", {
      access_token: "access-1",
      refresh_token: "refresh-1",
      expires_in: 3600,
      token_type: "Bearer",
      id_token: "ignored.jwt.token",
    });

    expect(assign).toHaveBeenCalledWith("/documents/xyz");

    const [tokenUrl, tokenInit] = fetchMock.mock.calls[0];
    expect(tokenUrl).toBe(`${ORIGIN}/oauth/token`);
    const body = tokenInit.body as URLSearchParams;
    expect(body.get("grant_type")).toBe("authorization_code");
    expect(body.get("code")).toBe("abc123");
    expect(body.get("client_id")).toBe("docstore-web");
    expect(body.has("resource")).toBe(false);
  });

  it("stores the access token in memory and the refresh token in sessionStorage, never persisting the access token", async () => {
    await completeCallback("/", {
      access_token: "super-secret-access-token",
      refresh_token: "the-refresh-token",
      expires_in: 3600,
      token_type: "Bearer",
      id_token: "ignored.jwt.token",
    });

    const storedValues = Object.keys(sessionStorage).map((k) => sessionStorage.getItem(k));
    expect(storedValues).toContain("the-refresh-token");
    expect(storedValues).not.toContain("super-secret-access-token");

    // A subsequent getAccessToken should return the in-memory token without another network
    // call, proving it really was captured in memory rather than lost.
    fetchMock.mockClear();
    const token = await oauth.getAccessToken();
    expect(token).toBe("super-secret-access-token");
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("falls back to a fresh login when the returned state does not match", async () => {
    await performLogin("/somewhere");

    const assign = setLocation({
      href: `${ORIGIN}/auth/callback?code=abc&state=wrong-state`,
      pathname: "/auth/callback",
      search: "?code=abc&state=wrong-state",
    });

    await oauth.handleCallback();

    expect(fetchMock).not.toHaveBeenCalled();
    expect(assign).toHaveBeenCalledTimes(1);
    expect((assign.mock.calls[0][0] as string).startsWith(`${ORIGIN}/oauth/authorize`)).toBe(true);
  });
});

describe("getAccessToken single-flight refresh", () => {
  it("issues exactly one refresh request for two concurrent callers", async () => {
    vi.useFakeTimers();
    await completeCallback("/", {
      access_token: "at-initial",
      refresh_token: "rt-initial",
      expires_in: 100,
      token_type: "Bearer",
    });

    // Jump past the 30%-remaining threshold (71s of a 100s lifetime elapsed).
    vi.advanceTimersByTime(71_000);

    fetchMock.mockClear();
    let resolveRefresh!: (resp: Response) => void;
    fetchMock.mockImplementationOnce(
      () =>
        new Promise<Response>((resolve) => {
          resolveRefresh = resolve;
        })
    );

    const first = oauth.getAccessToken();
    const second = oauth.getAccessToken();

    resolveRefresh(
      jsonResponse(200, {
        access_token: "at-refreshed",
        refresh_token: "rt-refreshed",
        expires_in: 100,
        token_type: "Bearer",
      })
    );

    const [tokenA, tokenB] = await Promise.all([first, second]);

    expect(tokenA).toBe("at-refreshed");
    expect(tokenB).toBe("at-refreshed");
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it("clears tokens and starts a fresh login when the refresh request fails", async () => {
    vi.useFakeTimers();
    await completeCallback("/", {
      access_token: "at-initial",
      refresh_token: "rt-initial",
      expires_in: 100,
      token_type: "Bearer",
    });
    vi.advanceTimersByTime(71_000);

    fetchMock.mockClear();
    fetchMock.mockResolvedValueOnce(jsonResponse(400, { error: "invalid_grant" }));

    const assign = setLocation({ pathname: "/protected", href: `${ORIGIN}/protected` });

    await expect(oauth.getAccessToken()).rejects.toThrow();

    expect(assign).toHaveBeenCalledTimes(1);
    expect((assign.mock.calls[0][0] as string).startsWith(`${ORIGIN}/oauth/authorize`)).toBe(true);
    expect(oauth.isAuthenticated()).toBe(false);
  });
});

describe("logout", () => {
  it("posts a revoke request for the refresh token and clears local state", async () => {
    await completeCallback("/", {
      access_token: "at-1",
      refresh_token: "rt-1",
      expires_in: 3600,
      token_type: "Bearer",
    });

    fetchMock.mockClear();
    fetchMock.mockResolvedValueOnce(jsonResponse(200, {}));

    await oauth.logout();

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe(`${ORIGIN}/oauth/revoke`);
    const body = init.body as URLSearchParams;
    expect(body.get("token")).toBe("rt-1");
    expect(body.get("token_type_hint")).toBe("refresh_token");
    expect(body.get("client_id")).toBe("docstore-web");

    expect(oauth.isAuthenticated()).toBe(false);
  });

  it("still clears local state when the revoke request fails", async () => {
    await completeCallback("/", {
      access_token: "at-1",
      refresh_token: "rt-1",
      expires_in: 3600,
      token_type: "Bearer",
    });

    fetchMock.mockClear();
    fetchMock.mockRejectedValueOnce(new Error("network down"));

    await expect(oauth.logout()).resolves.toBeUndefined();
    expect(oauth.isAuthenticated()).toBe(false);
  });
});

describe("isAuthenticated", () => {
  it("is false with no tokens and true once a refresh token has been stashed", async () => {
    expect(oauth.isAuthenticated()).toBe(false);
    await completeCallback("/", {
      access_token: "at-1",
      refresh_token: "rt-1",
      expires_in: 3600,
      token_type: "Bearer",
    });
    expect(oauth.isAuthenticated()).toBe(true);
  });
});
