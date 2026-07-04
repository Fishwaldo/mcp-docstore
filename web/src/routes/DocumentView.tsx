import { useState } from "react";
import { useParams, Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { ChevronDown } from "lucide-react";
import { getDocument, listSnapshots } from "@/lib/api";

export default function DocumentView() {
  const { id } = useParams<{ id: string }>();
  const [overviewOpen, setOverviewOpen] = useState(true);

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

  return (
    <div className="grid grid-cols-1 gap-8 p-8 lg:grid-cols-[minmax(0,1fr)_16rem]">
      {/* Main content */}
      <article className="min-w-0">
        <h1 className="text-2xl font-bold text-foreground mb-6 break-words">{doc.title}</h1>
        <div
          className="prose prose-sm dark:prose-invert max-w-none break-words"
          dangerouslySetInnerHTML={{ __html: doc.body_html }}
        />
      </article>

      {/* Right rail */}
      <aside className="min-w-0 lg:sticky lg:top-8 lg:self-start">
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
      </aside>
    </div>
  );
}
