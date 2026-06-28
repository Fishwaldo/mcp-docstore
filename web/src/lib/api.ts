// API client for DocStore web UI.
// All requests use credentials:"include" (session cookie).
// Non-GET requests include the CSRF double-submit header from the ds_csrf cookie.
// A 401 response triggers a redirect to /auth/login.

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

function getCSRFToken(): string {
  const match = document.cookie
    .split(";")
    .map((c) => c.trim())
    .find((c) => c.startsWith("ds_csrf="));
  return match ? match.slice("ds_csrf=".length) : "";
}

async function apiFetch<T>(path: string, init: RequestInit = {}): Promise<T> {
  const method = (init.method ?? "GET").toUpperCase();
  const headers = new Headers(init.headers);
  headers.set("Accept", "application/json");
  if (method !== "GET" && method !== "HEAD" && method !== "OPTIONS") {
    const token = getCSRFToken();
    if (token) {
      headers.set("X-CSRF-Token", token);
    }
  }
  const resp = await fetch(`/api${path}`, {
    ...init,
    method,
    credentials: "include",
    headers,
  });
  if (resp.status === 401) {
    window.location.href = "/auth/login";
    throw new Error("Unauthenticated");
  }
  if (!resp.ok) {
    const text = await resp.text().catch(() => resp.statusText);
    throw new Error(`API error ${resp.status}: ${text}`);
  }
  return resp.json() as Promise<T>;
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
