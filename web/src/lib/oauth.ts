// Public-client OAuth Authorization Code + PKCE flow against this server's own embedded
// authorization server (see internal/oauthsrv). There is no client secret: docstore-web is a
// public client (token_endpoint_auth_method: none), so PKCE is what proves possession of the
// authorization code. Tokens never touch a cookie or the backend session store — the access
// token lives only in a module-scoped variable (gone on reload/tab close) and the refresh token
// lives in sessionStorage (gone on tab close, not shared across tabs). This module has no
// framework dependency (no React, no router) so it can be exercised directly from tests and
// reused from any UI entry point.

export const clientId = "docstore-web";
export const origin = window.location.origin;
export const authorizeEndpoint = `${origin}/oauth/authorize`;
export const tokenEndpoint = `${origin}/oauth/token`;
export const revokeEndpoint = `${origin}/oauth/revoke`;
export const redirectUri = `${origin}/auth/callback`;
export const scopes = "openid profile email groups offline_access";

const PKCE_STASH_KEY = "docstore.oauth.pkce";
const REFRESH_TOKEN_KEY = "docstore.oauth.refresh_token";

// REFRESH_THRESHOLD_RATIO controls how early getAccessToken proactively refreshes: once less
// than this fraction of the token's original lifetime remains, refresh eagerly instead of
// waiting for the API to reject it, so ordinary requests rarely race a 401.
const REFRESH_THRESHOLD_RATIO = 0.3;

interface TokenResponse {
  access_token: string;
  refresh_token?: string;
  expires_in: number;
  token_type: string;
  // id_token is intentionally never read: this app has no need for the identity claims it
  // carries (identity comes from /api/me) and parsing/verifying it client-side would just be
  // more attack surface for no benefit.
  id_token?: string;
  scope?: string;
}

interface PkceStash {
  verifier: string;
  state: string;
  returnTo: string;
}

let accessToken: string | null = null;
let accessTokenExpiresAt: number | null = null;
let accessTokenLifetimeMs = 0;
let refreshPromise: Promise<string> | null = null;

function currentPath(): string {
  return window.location.pathname + window.location.search;
}

function base64UrlEncode(bytes: Uint8Array): string {
  let binary = "";
  for (const byte of bytes) {
    binary += String.fromCharCode(byte);
  }
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

function randomBase64Url(byteLength: number): string {
  const bytes = new Uint8Array(byteLength);
  crypto.getRandomValues(bytes);
  return base64UrlEncode(bytes);
}

async function pkceChallenge(verifier: string): Promise<string> {
  const data = new TextEncoder().encode(verifier);
  const digest = await crypto.subtle.digest("SHA-256", data);
  return base64UrlEncode(new Uint8Array(digest));
}

function storeTokens(tokens: TokenResponse): void {
  accessToken = tokens.access_token;
  accessTokenLifetimeMs = tokens.expires_in * 1000;
  accessTokenExpiresAt = Date.now() + accessTokenLifetimeMs;
  if (tokens.refresh_token) {
    sessionStorage.setItem(REFRESH_TOKEN_KEY, tokens.refresh_token);
  }
}

function clearTokens(): void {
  accessToken = null;
  accessTokenExpiresAt = null;
  accessTokenLifetimeMs = 0;
  sessionStorage.removeItem(REFRESH_TOKEN_KEY);
}

function isExpiringSoon(): boolean {
  if (accessToken === null || accessTokenExpiresAt === null) {
    return true;
  }
  const threshold = accessTokenLifetimeMs * REFRESH_THRESHOLD_RATIO;
  return Date.now() >= accessTokenExpiresAt - threshold;
}

// login stashes a fresh PKCE verifier/state/returnTo triple in sessionStorage and redirects the
// browser to /oauth/authorize. There is no "resource" parameter: this server mints tokens for
// itself only, never on behalf of another audience.
export async function login(returnTo: string): Promise<void> {
  const verifier = randomBase64Url(32);
  const state = randomBase64Url(16);
  const challenge = await pkceChallenge(verifier);

  const stash: PkceStash = { verifier, state, returnTo };
  sessionStorage.setItem(PKCE_STASH_KEY, JSON.stringify(stash));

  const params = new URLSearchParams({
    response_type: "code",
    client_id: clientId,
    redirect_uri: redirectUri,
    scope: scopes,
    state,
    code_challenge: challenge,
    code_challenge_method: "S256",
  });
  window.location.assign(`${authorizeEndpoint}?${params.toString()}`);
}

// AuthCallbackError is thrown by handleCallback when a sign-in cannot be completed. The
// /auth/callback view renders it as a terminal error with a manual retry, deliberately NOT
// auto-restarting login: an authorization server that keeps denying (e.g. error=access_denied,
// or a user without upstream access) would otherwise spin authorize→callback→login forever.
export class AuthCallbackError extends Error {
  readonly code: string;
  constructor(code: string, message: string) {
    super(message);
    this.name = "AuthCallbackError";
    this.code = code;
  }
}

// handleCallback runs on the /auth/callback route: it validates the state the AS echoed back
// against the one stashed by login, exchanges the code for tokens, and navigates to whatever
// page the user originally wanted. On any failure it throws AuthCallbackError — it never
// restarts login itself, so a persistently failing/denying provider cannot produce a redirect
// loop; the callback view surfaces the error and offers a user-initiated retry.
export async function handleCallback(): Promise<void> {
  const url = new URL(window.location.href);
  const code = url.searchParams.get("code");
  const state = url.searchParams.get("state");
  const authError = url.searchParams.get("error");

  const stashRaw = sessionStorage.getItem(PKCE_STASH_KEY);
  sessionStorage.removeItem(PKCE_STASH_KEY);

  if (authError) {
    const desc = url.searchParams.get("error_description");
    throw new AuthCallbackError(
      authError,
      desc ? `${authError}: ${desc}` : `Sign-in was rejected (${authError}).`,
    );
  }

  if (!code || !state || !stashRaw) {
    throw new AuthCallbackError(
      "invalid_callback",
      "This sign-in link is no longer valid. Please start over.",
    );
  }

  let stash: PkceStash;
  try {
    stash = JSON.parse(stashRaw) as PkceStash;
  } catch {
    throw new AuthCallbackError(
      "invalid_callback",
      "This sign-in link is no longer valid. Please start over.",
    );
  }

  if (state !== stash.state) {
    throw new AuthCallbackError(
      "state_mismatch",
      "Sign-in could not be verified. Please start over.",
    );
  }

  const body = new URLSearchParams({
    grant_type: "authorization_code",
    code,
    redirect_uri: redirectUri,
    client_id: clientId,
    code_verifier: stash.verifier,
  });

  const resp = await fetch(tokenEndpoint, {
    method: "POST",
    headers: { "Content-Type": "application/x-www-form-urlencoded" },
    body,
  });

  if (!resp.ok) {
    throw new AuthCallbackError(
      "token_exchange_failed",
      "Could not complete sign-in. Please try again.",
    );
  }

  const tokens = (await resp.json()) as TokenResponse;
  storeTokens(tokens);

  window.location.assign(stash.returnTo || "/");
}

async function performRefresh(): Promise<string> {
  const refreshToken = sessionStorage.getItem(REFRESH_TOKEN_KEY);
  if (!refreshToken) {
    clearTokens();
    await login(currentPath());
    throw new Error("no refresh token available");
  }

  const body = new URLSearchParams({
    grant_type: "refresh_token",
    client_id: clientId,
    refresh_token: refreshToken,
  });

  let resp: Response;
  try {
    resp = await fetch(tokenEndpoint, {
      method: "POST",
      headers: { "Content-Type": "application/x-www-form-urlencoded" },
      body,
    });
  } catch (err) {
    clearTokens();
    await login(currentPath());
    throw err instanceof Error ? err : new Error("refresh request failed");
  }

  if (!resp.ok) {
    clearTokens();
    await login(currentPath());
    throw new Error(`refresh failed: ${resp.status}`);
  }

  const tokens = (await resp.json()) as TokenResponse;
  storeTokens(tokens);
  return accessToken as string;
}

// doRefresh is the single-flight gate: every caller that needs a refresh while one is already
// in progress awaits the same promise instead of firing its own token-endpoint request. Without
// this, several components mounting at once (sidebar, document view, search) would each hit
// /oauth/token the moment the access token goes stale.
function doRefresh(): Promise<string> {
  if (!refreshPromise) {
    refreshPromise = performRefresh().finally(() => {
      refreshPromise = null;
    });
  }
  return refreshPromise;
}

// getAccessToken returns a token that is safe to use right now: the current one if it still has
// most of its life left, otherwise the result of a (possibly shared) refresh.
export async function getAccessToken(): Promise<string> {
  if (accessToken !== null && !isExpiringSoon()) {
    return accessToken;
  }
  return doRefresh();
}

// refreshAccessToken forces a refresh regardless of the in-memory expiry estimate. api.ts calls
// this after an unexpected 401 — the server rejected a token this module still believed was
// good (clock skew, revocation, etc.) — before giving up and sending the user to log in again.
export async function refreshAccessToken(): Promise<string> {
  return doRefresh();
}

// logout revokes the refresh token server-side and clears all local state. It proceeds with the
// local clear even if the network call fails: the user has still signed out of this SPA, even
// if the AS never gets the revoke request.
export async function logout(): Promise<void> {
  const refreshToken = sessionStorage.getItem(REFRESH_TOKEN_KEY);
  clearTokens();
  if (!refreshToken) {
    return;
  }
  try {
    await fetch(revokeEndpoint, {
      method: "POST",
      headers: { "Content-Type": "application/x-www-form-urlencoded" },
      body: new URLSearchParams({
        token: refreshToken,
        token_type_hint: "refresh_token",
        client_id: clientId,
      }),
    });
  } catch {
    // best-effort: local state is already cleared above.
  }
}

// isAuthenticated reports whether this tab has any means of producing a valid access token
// right now: either one already in memory, or a refresh token to trade for one. It does not
// itself make a network call.
export function isAuthenticated(): boolean {
  return accessToken !== null || sessionStorage.getItem(REFRESH_TOKEN_KEY) !== null;
}
