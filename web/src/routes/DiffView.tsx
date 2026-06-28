import { useParams, useSearchParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { parseDiff, Diff, Hunk } from "react-diff-view";
import "react-diff-view/style/index.css";
import { diffVersions } from "@/lib/api";

export default function DiffView() {
  const { id } = useParams<{ id: string }>();
  const [searchParams] = useSearchParams();
  const from = searchParams.get("from");
  const to = searchParams.get("to");

  const {
    data: diff,
    isLoading,
    isError,
  } = useQuery({
    queryKey: ["diff", id, from, to],
    queryFn: () => diffVersions(id!, Number(from), Number(to)),
    enabled: !!id && !!from && !!to,
  });

  if (isLoading) {
    return (
      <div className="p-8 space-y-4 animate-pulse">
        <div className="h-6 bg-muted rounded w-1/4" />
        <div className="h-48 bg-muted rounded" />
      </div>
    );
  }

  if (isError) {
    return <div className="p-8 text-destructive">Failed to load diff.</div>;
  }

  const files = diff ? parseDiff(diff.diff) : [];

  return (
    <div className="p-8">
      <h1 className="text-xl font-semibold text-foreground mb-6">
        Diff: version {from} → {to}
      </h1>

      {files.length === 0 ? (
        <p className="text-muted-foreground">No differences.</p>
      ) : (
        <div className="space-y-6">
          {files.map((file, i) => (
            <div key={i} className="rounded-md border border-border overflow-hidden">
              <Diff diffType={file.type} hunks={file.hunks} viewType="split">
                {(hunks) =>
                  hunks.map((hunk) => <Hunk key={hunk.content} hunk={hunk} />)
                }
              </Diff>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
