import { Link, useSearchParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { searchDocuments } from "@/lib/api";

export default function SearchResults() {
  const [searchParams] = useSearchParams();
  const q = searchParams.get("q") ?? "";

  const { data: hits, isLoading, isError } = useQuery({
    queryKey: ["search", q],
    queryFn: () => searchDocuments({ q }),
    enabled: !!q,
  });

  return (
    <div className="p-8 max-w-3xl">
      <h1 className="text-xl font-semibold text-foreground mb-6">
        Search results for <span className="text-primary">&ldquo;{q}&rdquo;</span>
      </h1>

      {isLoading && (
        <div className="space-y-4 animate-pulse">
          {[1, 2, 3].map((n) => (
            <div key={n} className="space-y-2">
              <div className="h-5 bg-muted rounded w-1/3" />
              <div className="h-4 bg-muted rounded w-full" />
            </div>
          ))}
        </div>
      )}

      {isError && (
        <p className="text-destructive">Search failed.</p>
      )}

      {hits && hits.length === 0 && (
        <p className="text-muted-foreground">No results found.</p>
      )}

      {hits && hits.length > 0 && (
        <ul className="space-y-5">
          {hits.map((hit) => (
            <li key={hit.document_id}>
              <div className="flex items-center gap-2 mb-1">
                <Link
                  to={`/documents/${hit.document_id}`}
                  className="text-base font-medium text-primary hover:underline"
                >
                  {hit.title}
                </Link>
                <span className="inline-flex items-center rounded-full bg-muted px-2 py-0.5 text-xs text-muted-foreground">
                  {hit.project_id}
                </span>
              </div>
              {hit.snippet && (
                <p
                  className="text-sm text-foreground"
                  dangerouslySetInnerHTML={{ __html: hit.snippet }}
                />
              )}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
