// API client for DocStore web UI.
// Every request carries an Authorization: Bearer header from oauth.getAccessToken(). A 401
// forces one token refresh and retries the request exactly once; a second 401 starts a fresh
// login. A 403 with {"error":"no_access"} means the token is valid but its owner isn't
// provisioned for any tenant — that must NOT redirect to login (the AS would just hand back a
// token for the same unprovisioned account, looping forever), so it surfaces as a distinct
// ApiNoAccessError the UI renders as a no-access screen.

import { getAccessToken, refreshAccessToken, login } from "./oauth";

// NO_ACCESS_EVENT is dispatched on window whenever a request hits the no_access case, so a
// top-level component can show the no-access screen without every call site wiring its own
// handling of ApiNoAccessError.
export const NO_ACCESS_EVENT = "docstore:no-access";

export class ApiNoAccessError extends Error {
  constructor() {
    super("no_access");
    this.name = "ApiNoAccessError";
  }
}

export class ConflictError extends Error {
  currentVersion: number;
  constructor(currentVersion: number, message: string) {
    super(message);
    this.name = "ConflictError";
    this.currentVersion = currentVersion;
  }
}

export interface MeDTO {
  email: string;
  tenant: string;
  groups: string[];
}

export interface ProjectDTO {
  id: string;
  name: string;
  description: string;
  visibility: string;
  archived: boolean;
  access?: string;
}

export interface DocumentSummaryDTO {
  id: string;
  title: string;
  overview: string;
  tags: string[];
  version: number;
  updated_at: string;
}

export interface DocumentDTO {
  id: string;
  project_id?: string;
  title: string;
  overview: string;
  body: string;
  tags: string[];
  version: number;
  change_comment: string;
  updated_at: string;
  body_html: string;
}

export interface SnapshotDTO {
  version: number;
  comment: string;
  created_by: string;
  created_at: string;
  body_html?: string;
}

export interface SearchHitDTO {
  document_id: string;
  project_id: string;
  title: string;
  overview: string;
  score: number;
  snippet: string;
}

export interface SectionDTO {
  heading: string;
  html: string;
}

export interface DiffDTO {
  diff: string;
}

async function doFetch(path: string, method: string, init: RequestInit, token: string): Promise<Response> {
  const headers = new Headers(init.headers);
  headers.set("Accept", "application/json");
  headers.set("Authorization", `Bearer ${token}`);
  return fetch(`/api${path}`, { ...init, method, headers });
}

async function apiFetch<T>(path: string, init: RequestInit = {}): Promise<T> {
  const method = (init.method ?? "GET").toUpperCase();

  let token = await getAccessToken();
  let resp = await doFetch(path, method, init, token);

  if (resp.status === 401) {
    token = await refreshAccessToken();
    resp = await doFetch(path, method, init, token);
    if (resp.status === 401) {
      await login(window.location.pathname + window.location.search);
      throw new Error("Unauthenticated");
    }
  }

  if (resp.status === 403) {
    const body = (await resp.json().catch(() => null)) as { error?: string } | null;
    if (body?.error === "no_access") {
      window.dispatchEvent(new CustomEvent(NO_ACCESS_EVENT));
      throw new ApiNoAccessError();
    }
    throw new Error(`API error 403: ${JSON.stringify(body)}`);
  }

  if (resp.status === 409) {
    const body = (await resp.json().catch(() => null)) as { detail?: string } | null;
    const detail = body?.detail ?? "version conflict";
    const m = /current version is (\d+)/.exec(detail);
    throw new ConflictError(m ? Number(m[1]) : 0, detail);
  }

  if (!resp.ok) {
    const text = await resp.text().catch(() => resp.statusText);
    throw new Error(`API error ${resp.status}: ${text}`);
  }

  if (resp.status === 204) return undefined as T;
  return resp.json() as Promise<T>;
}

export async function getMe(): Promise<MeDTO> {
  return apiFetch<MeDTO>("/me");
}

export async function listProjects(includeArchived = false): Promise<ProjectDTO[]> {
  return apiFetch<ProjectDTO[]>(`/projects${includeArchived ? "?include_archived=true" : ""}`);
}

export async function getProject(id: string): Promise<ProjectDTO> {
  return apiFetch<ProjectDTO>(`/projects/${id}`);
}

export async function listDocuments(projectId: string): Promise<DocumentSummaryDTO[]> {
  return apiFetch<DocumentSummaryDTO[]>(`/projects/${projectId}/documents`);
}

export async function getDocument(id: string): Promise<DocumentDTO> {
  return apiFetch<DocumentDTO>(`/documents/${id}`);
}

export async function getSection(id: string, heading: string): Promise<SectionDTO> {
  return apiFetch<SectionDTO>(`/documents/${id}/section?heading=${encodeURIComponent(heading)}`);
}

export async function listSnapshots(id: string): Promise<SnapshotDTO[]> {
  return apiFetch<SnapshotDTO[]>(`/documents/${id}/snapshots`);
}

export async function getSnapshot(id: string, version: number): Promise<SnapshotDTO> {
  return apiFetch<SnapshotDTO>(`/documents/${id}/snapshots/${version}`);
}

export async function diffVersions(id: string, from: number, to: number): Promise<DiffDTO> {
  return apiFetch<DiffDTO>(`/documents/${id}/diff?from=${from}&to=${to}`);
}

export interface SearchParams {
  q: string;
  projectId?: string;
  visibility?: string;
  tags?: string[];
  limit?: number;
}

export async function searchDocuments(params: SearchParams): Promise<SearchHitDTO[]> {
  const p = new URLSearchParams({ q: params.q });
  if (params.projectId) p.set("project_id", params.projectId);
  if (params.visibility) p.set("visibility", params.visibility);
  if (params.tags) params.tags.forEach((t) => p.append("tags", t));
  if (params.limit) p.set("limit", String(params.limit));
  return apiFetch<SearchHitDTO[]>(`/search?${p.toString()}`);
}

export interface EditDocumentInput {
  base_version: number;
  overview: string;
  body: string;
  tags: string[];
  comment?: string;
}

export interface CreateDocumentInput {
  project_id: string;
  title: string;
  overview?: string;
  body?: string;
  tags?: string[];
}

export interface RestoreSnapshotInput {
  version: number;
  base_version: number;
  scope?: "body" | "full";
  comment?: string;
}

export function editDocument(id: string, input: EditDocumentInput): Promise<DocumentDTO> {
  return apiFetch<DocumentDTO>(`/documents/${id}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
}

export function createDocument(input: CreateDocumentInput): Promise<DocumentDTO> {
  return apiFetch<DocumentDTO>(`/documents`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
}

export async function deleteDocument(id: string): Promise<void> {
  await apiFetch<void>(`/documents/${id}`, { method: "DELETE" });
}

export function restoreSnapshot(id: string, input: RestoreSnapshotInput): Promise<DocumentDTO> {
  return apiFetch<DocumentDTO>(`/documents/${id}/restore`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
}

export async function listTags(): Promise<string[]> {
  const out = await apiFetch<{ tags: string[] }>(`/tags`);
  return out.tags;
}
