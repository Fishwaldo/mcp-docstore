import { useState } from "react";
import { useParams, Link } from "react-router-dom";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { ChevronDown } from "lucide-react";
import { getDocument, listSnapshots, getProject, editDocument, ConflictError } from "@/lib/api";
import MarkdownEditor from "@/components/MarkdownEditor";
import TagEditor from "@/components/TagEditor";

function arraysEqual(a: string[], b: string[]): boolean {
  if (a.length !== b.length) return false;
  const sortedA = [...a].sort();
  const sortedB = [...b].sort();
  return sortedA.every((v, i) => v === sortedB[i]);
}

export default function DocumentView() {
  const { id } = useParams<{ id: string }>();
  const queryClient = useQueryClient();
  const [overviewOpen, setOverviewOpen] = useState(true);

  const [mode, setMode] = useState<"view" | "edit">("view");
  const [body, setBody] = useState("");
  const [overview, setOverview] = useState("");
  const [tags, setTags] = useState<string[]>([]);
  const [conflict, setConflict] = useState<number | null>(null);
  const [saveError, setSaveError] = useState<string | null>(null);

  const {
    data: doc,
    isLoading: docLoading,
    isError: docError,
  } = useQuery({
    queryKey: ["document", id],
    queryFn: () => getDocument(id!),
    enabled: !!id,
  });

  const { data: snapshots } = useQuery({
    queryKey: ["snapshots", id],
    queryFn: () => listSnapshots(id!),
    enabled: !!id,
  });

  const { data: project } = useQuery({
    queryKey: ["project", doc?.project_id],
    queryFn: () => getProject(doc!.project_id!),
    enabled: !!doc?.project_id,
  });

  const canEdit = project?.access === "write";

  const saveMutation = useMutation({
    mutationFn: (input: { base_version: number; overview: string; body: string; tags: string[] }) =>
      editDocument(doc!.id, input),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["document", id] });
      queryClient.invalidateQueries({ queryKey: ["snapshots", id] });
      setConflict(null);
      setSaveError(null);
      setMode("view");
    },
    onError: (err: unknown) => {
      if (err instanceof ConflictError) {
        setConflict(err.currentVersion);
      } else {
        setSaveError(err instanceof Error ? err.message : "Failed to save.");
      }
    },
  });

  if (docLoading) {
    return (
      <div className="p-8 space-y-4 animate-pulse">
        <div className="h-8 bg-muted rounded w-1/3" />
        <div className="h-4 bg-muted rounded w-full" />
        <div className="h-4 bg-muted rounded w-5/6" />
        <div className="h-4 bg-muted rounded w-4/5" />
      </div>
    );
  }

  if (docError || !doc) {
    return (
      <div className="p-8 text-destructive">
        Failed to load document.
      </div>
    );
  }

  function startEdit() {
    setBody(doc!.body);
    setOverview(doc!.overview);
    setTags([...doc!.tags]);
    setConflict(null);
    setSaveError(null);
    setMode("edit");
  }

  function cancelEdit() {
    setConflict(null);
    setSaveError(null);
    setMode("view");
  }

  function handleSave() {
    const changed =
      body !== doc!.body || overview !== doc!.overview || !arraysEqual(tags, doc!.tags);
    if (!changed) {
      setMode("view");
      return;
    }
    saveMutation.mutate({ base_version: doc!.version, overview, body, tags });
  }

  function handleReload() {
    queryClient.invalidateQueries({ queryKey: ["document", id] });
    setConflict(null);
    setMode("view");
  }

  function handleKeepEditing() {
    setConflict(null);
  }

  return (
    <div className="grid grid-cols-1 gap-8 p-8 lg:grid-cols-[minmax(0,1fr)_16rem]">
      {/* Main content */}
      <article className="min-w-0">
        {mode === "view" ? (
          <>
            <div className="flex items-center justify-between gap-4 mb-6">
              <h1 className="text-2xl font-bold text-foreground break-words">{doc.title}</h1>
              {canEdit && (
                <button
                  type="button"
                  onClick={startEdit}
                  className="shrink-0 inline-flex items-center justify-center rounded-md border border-border px-3 py-1.5 text-sm font-medium text-foreground hover:bg-accent"
                >
                  Edit
                </button>
              )}
            </div>
            <div
              className="prose prose-sm dark:prose-invert max-w-none break-words"
              dangerouslySetInnerHTML={{ __html: doc.body_html }}
            />
          </>
        ) : (
          <>
            <h1 className="text-2xl font-bold text-foreground mb-6 break-words">{doc.title}</h1>
            <MarkdownEditor markdown={body} onChange={setBody} />
          </>
        )}
      </article>

      {/* Right rail */}
      <aside className="min-w-0 lg:sticky lg:top-8 lg:self-start">
        {mode === "view" ? (
          <div className="space-y-6">
            {/* Overview */}
            {doc.overview && (
              <section>
                <button
                  type="button"
                  onClick={() => setOverviewOpen((v) => !v)}
                  aria-expanded={overviewOpen}
                  className="flex w-full items-center justify-between gap-2 mb-2 text-xs font-semibold text-muted-foreground uppercase tracking-wider hover:text-foreground"
                >
                  <span>Overview</span>
                  <ChevronDown
                    className={`h-3.5 w-3.5 shrink-0 transition-transform ${overviewOpen ? "" : "-rotate-90"}`}
                  />
                </button>
                {overviewOpen && (
                  <p className="text-sm text-foreground break-words">{doc.overview}</p>
                )}
              </section>
            )}

            {/* Tags */}
            {doc.tags.length > 0 && (
              <section>
                <h2 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-2">
                  Tags
                </h2>
                <div className="flex flex-wrap gap-1.5">
                  {doc.tags.map((tag) => (
                    <span
                      key={tag}
                      className="inline-flex items-center rounded-full border border-border px-2.5 py-0.5 text-xs font-medium text-foreground"
                    >
                      {tag}
                    </span>
                  ))}
                </div>
              </section>
            )}

            {/* Metadata */}
            <section>
              <h2 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-2">
                Info
              </h2>
              <dl className="space-y-1 text-sm">
                <div className="flex gap-2">
                  <dt className="text-muted-foreground">Version</dt>
                  <dd className="text-foreground font-medium">{doc.version}</dd>
                </div>
                <div className="flex gap-2">
                  <dt className="text-muted-foreground">Updated</dt>
                  <dd className="text-foreground">
                    {new Date(doc.updated_at).toLocaleDateString()}
                  </dd>
                </div>
              </dl>
            </section>

            {/* Version history */}
            {snapshots && snapshots.length > 0 && (
              <section>
                <h2 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-2">
                  Version History
                </h2>
                <ul className="space-y-3">
                  {snapshots.map((snap) => (
                    <li key={snap.version} className="text-sm">
                      <div className="flex items-center justify-between gap-2">
                        <span className="font-medium text-foreground">v{snap.version}</span>
                        <Link
                          to={`/documents/${id}/diff?from=${snap.version}&to=${doc.version}`}
                          className="text-xs text-primary hover:underline"
                        >
                          Diff vs current
                        </Link>
                      </div>
                      {snap.comment && (
                        <p className="text-muted-foreground truncate">{snap.comment}</p>
                      )}
                      <p className="text-xs text-muted-foreground">
                        {new Date(snap.created_at).toLocaleDateString()}
                      </p>
                    </li>
                  ))}
                </ul>
              </section>
            )}
          </div>
        ) : (
          <div className="space-y-6">
            {conflict !== null && (
              <div className="rounded-md border border-destructive/50 bg-destructive/10 p-3 text-sm space-y-2">
                <p className="text-foreground">
                  This document changed since you opened it (current version {conflict}).
                </p>
                <div className="flex gap-2">
                  <button
                    type="button"
                    onClick={handleReload}
                    className="rounded-md bg-primary px-2.5 py-1 text-xs font-medium text-primary-foreground hover:opacity-90"
                  >
                    Reload
                  </button>
                  <button
                    type="button"
                    onClick={handleKeepEditing}
                    className="rounded-md border border-border px-2.5 py-1 text-xs font-medium text-foreground hover:bg-accent"
                  >
                    Keep editing
                  </button>
                </div>
              </div>
            )}

            <section>
              <h2 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-2">
                Overview
              </h2>
              <textarea
                value={overview}
                onChange={(e) => setOverview(e.target.value)}
                rows={4}
                className="w-full rounded-md border border-border bg-background px-2 py-1.5 text-sm text-foreground"
              />
            </section>

            <section>
              <h2 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-2">
                Tags
              </h2>
              <TagEditor tags={tags} onChange={setTags} />
            </section>

            <section>
              <h2 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-2">
                Info
              </h2>
              <p className="text-sm text-foreground">v{doc.version}</p>
            </section>

            {saveError && <p className="text-sm text-destructive">{saveError}</p>}

            <div className="flex gap-2">
              <button
                type="button"
                onClick={handleSave}
                disabled={saveMutation.isPending}
                className="rounded-md bg-primary px-3 py-1.5 text-sm font-medium text-primary-foreground hover:opacity-90 disabled:opacity-50"
              >
                Save
              </button>
              <button
                type="button"
                onClick={cancelEdit}
                className="rounded-md border border-border px-3 py-1.5 text-sm font-medium text-foreground hover:bg-accent"
              >
                Cancel
              </button>
            </div>
          </div>
        )}
      </aside>
    </div>
  );
}
