import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { X } from "lucide-react";
import { addShares, listShares, removeShares, type AddSharesInput } from "@/lib/api";

type ShareKind = AddSharesInput["kind"];
type SharePermission = AddSharesInput["permission"];

export default function SharesPanel({ projectId }: { projectId: string }) {
  const queryClient = useQueryClient();

  const [kind, setKind] = useState<ShareKind>("user");
  const [principal, setPrincipal] = useState("");
  const [permission, setPermission] = useState<SharePermission>("read");
  const [unresolved, setUnresolved] = useState<string[]>([]);

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

  const addMutation = useMutation({
    mutationFn: (input: AddSharesInput) => addShares(projectId, input),
    onSuccess: (result) => {
      queryClient.invalidateQueries({ queryKey: ["shares", projectId] });
      setPrincipal("");
      setUnresolved(result.unresolved);
    },
  });

  function handleAdd() {
    const trimmed = principal.trim();
    if (!trimmed) {
      return;
    }
    setUnresolved([]);
    addMutation.mutate({ kind, principals: [trimmed], permission });
  }

  const addForm = (
    <div className="space-y-2 border-t border-border pt-3">
      <div
        role="radiogroup"
        aria-label="Share kind"
        className="flex items-center gap-0.5 rounded-md border border-input p-0.5 text-xs w-fit"
      >
        <button
          type="button"
          role="radio"
          onClick={() => setKind("user")}
          aria-checked={kind === "user"}
          className={`rounded px-2 py-1 ${
            kind === "user"
              ? "bg-accent text-foreground"
              : "text-muted-foreground hover:text-foreground"
          }`}
        >
          User
        </button>
        <button
          type="button"
          role="radio"
          onClick={() => setKind("group")}
          aria-checked={kind === "group"}
          className={`rounded px-2 py-1 ${
            kind === "group"
              ? "bg-accent text-foreground"
              : "text-muted-foreground hover:text-foreground"
          }`}
        >
          Group
        </button>
      </div>

      <input
        type="text"
        value={principal}
        onChange={(e) => setPrincipal(e.target.value)}
        placeholder={kind === "user" ? "Email address" : "Group name"}
        className="w-full rounded-md border border-input bg-background px-3 py-1.5 text-sm placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-ring"
      />

      <div className="flex items-center gap-2">
        <div
          role="radiogroup"
          aria-label="Permission"
          className="flex items-center gap-0.5 rounded-md border border-input p-0.5 text-xs"
        >
          <button
            type="button"
            role="radio"
            onClick={() => setPermission("read")}
            aria-checked={permission === "read"}
            className={`rounded px-2 py-1 ${
              permission === "read"
                ? "bg-accent text-foreground"
                : "text-muted-foreground hover:text-foreground"
            }`}
          >
            Read
          </button>
          <button
            type="button"
            role="radio"
            onClick={() => setPermission("write")}
            aria-checked={permission === "write"}
            className={`rounded px-2 py-1 ${
              permission === "write"
                ? "bg-accent text-foreground"
                : "text-muted-foreground hover:text-foreground"
            }`}
          >
            Write
          </button>
        </div>

        <button
          type="button"
          onClick={handleAdd}
          disabled={!principal.trim() || addMutation.isPending}
          className="ml-auto rounded-md bg-primary px-3 py-1.5 text-sm text-primary-foreground disabled:opacity-50"
        >
          Add
        </button>
      </div>

      {addMutation.isError && (
        <p className="text-sm text-destructive">Couldn&rsquo;t add share.</p>
      )}

      {unresolved.length > 0 && (
        <p className="text-sm text-amber-600 dark:text-amber-500">
          Couldn&rsquo;t resolve: {unresolved.join(", ")}
        </p>
      )}
    </div>
  );

  if (isLoading) {
    return <p className="text-muted-foreground">Loading shares…</p>;
  }

  if (isError || !shares) {
    return <p className="text-destructive">Couldn&rsquo;t load shares.</p>;
  }

  const empty = shares.users.length === 0 && shares.groups.length === 0;

  return (
    <div className="space-y-4">
      {empty && <p className="text-muted-foreground">No shares yet.</p>}

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

      {removeMutation.isError && (
        <p className="text-sm text-destructive">Couldn&rsquo;t remove share.</p>
      )}

      {addForm}
    </div>
  );
}
