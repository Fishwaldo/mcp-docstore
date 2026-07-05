import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { X } from "lucide-react";
import { listShares, removeShares } from "@/lib/api";

export default function SharesPanel({ projectId }: { projectId: string }) {
  const queryClient = useQueryClient();

  const {
    data: shares,
    isLoading,
    isError,
  } = useQuery({
    queryKey: ["shares", projectId],
    queryFn: () => listShares(projectId),
  });

  const removeMutation = useMutation({
    mutationFn: (input: { kind: "user" | "group"; principals: string[] }) =>
      removeShares(projectId, input),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["shares", projectId] });
    },
  });

  if (isLoading) {
    return <p className="text-muted-foreground">Loading shares…</p>;
  }

  if (isError || !shares) {
    return <p className="text-destructive">Couldn&rsquo;t load shares.</p>;
  }

  const empty = shares.users.length === 0 && shares.groups.length === 0;

  if (empty) {
    return <p className="text-muted-foreground">No shares yet.</p>;
  }

  return (
    <div className="space-y-4">
      {shares.users.length > 0 && (
        <div>
          <h3 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-2">
            Users
          </h3>
          <ul className="space-y-1">
            {shares.users.map((u) => (
              <li
                key={u.email}
                className="flex items-center justify-between gap-2 rounded-md px-2 py-1.5 hover:bg-accent"
              >
                <span className="text-sm text-foreground truncate">{u.email}</span>
                <div className="flex items-center gap-2 shrink-0">
                  <span className="inline-flex items-center rounded-full border border-border px-2.5 py-0.5 text-xs font-medium text-foreground">
                    {u.permission}
                  </span>
                  <button
                    type="button"
                    onClick={() =>
                      removeMutation.mutate({ kind: "user", principals: [u.email] })
                    }
                    aria-label={`Remove ${u.email}`}
                    className="text-muted-foreground hover:text-foreground"
                  >
                    <X className="h-3.5 w-3.5" />
                  </button>
                </div>
              </li>
            ))}
          </ul>
        </div>
      )}

      {shares.groups.length > 0 && (
        <div>
          <h3 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-2">
            Groups
          </h3>
          <ul className="space-y-1">
            {shares.groups.map((g) => (
              <li
                key={g.group}
                className="flex items-center justify-between gap-2 rounded-md px-2 py-1.5 hover:bg-accent"
              >
                <span className="text-sm text-foreground truncate">{g.group}</span>
                <div className="flex items-center gap-2 shrink-0">
                  <span className="inline-flex items-center rounded-full border border-border px-2.5 py-0.5 text-xs font-medium text-foreground">
                    {g.permission}
                  </span>
                  <button
                    type="button"
                    onClick={() =>
                      removeMutation.mutate({ kind: "group", principals: [g.group] })
                    }
                    aria-label={`Remove ${g.group}`}
                    className="text-muted-foreground hover:text-foreground"
                  >
                    <X className="h-3.5 w-3.5" />
                  </button>
                </div>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}
