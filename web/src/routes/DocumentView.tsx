import { useParams, Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { getDocument, listSnapshots } from "@/lib/api";

export default function DocumentView() {
  const { id } = useParams<{ id: string }>();

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
    <div className="flex gap-6 p-8">
      {/* Main content */}
      <article className="flex-1 min-w-0" style={{ flexBasis: "70%" }}>
        <h1 className="text-2xl font-bold text-foreground mb-6">{doc.title}</h1>
        <div
          className="prose prose-sm max-w-none"
          dangerouslySetInnerHTML={{ __html: doc.body_html }}
        />
      </article>

      {/* Right rail */}
      <aside className="shrink-0" style={{ flexBasis: "30%" }}>
        <div className="space-y-6 sticky top-8">
          {/* Overview */}
          {doc.overview && (
            <section>
              <h2 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-2">
                Overview
              </h2>
              <p className="text-sm text-foreground">{doc.overview}</p>
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
