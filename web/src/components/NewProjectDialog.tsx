import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import * as Dialog from "@radix-ui/react-dialog";
import { Plus } from "lucide-react";
import { createProject } from "@/lib/api";

type Visibility = "org" | "private";

export default function NewProjectDialog() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [visibility, setVisibility] = useState<Visibility>("org");
  const [description, setDescription] = useState("");
  const [error, setError] = useState<string | null>(null);

  const createMutation = useMutation({
    mutationFn: () => createProject({ name: name.trim(), visibility, description }),
    onSuccess: (created) => {
      queryClient.invalidateQueries({ queryKey: ["projects"] });
      setOpen(false);
      navigate(`/projects/${created.id}`);
    },
    onError: (err: unknown) => {
      setError(err instanceof Error ? err.message : "Failed to create project.");
    },
  });

  function handleOpenChange(next: boolean) {
    setOpen(next);
    if (next) {
      setName("");
      setVisibility("org");
      setDescription("");
      setError(null);
    }
  }

  return (
    <Dialog.Root open={open} onOpenChange={handleOpenChange}>
      <Dialog.Trigger asChild>
        <button
          type="button"
          aria-label="New project"
          title="New project"
          className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md border border-border text-muted-foreground hover:bg-accent hover:text-foreground"
        >
          <Plus className="h-4 w-4" />
        </button>
      </Dialog.Trigger>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 bg-black/50" />
        <Dialog.Content className="fixed left-1/2 top-1/2 w-full max-w-sm -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-background p-6 shadow-lg">
          <Dialog.Title className="text-lg font-semibold text-foreground">
            New project
          </Dialog.Title>

          <div className="mt-4 space-y-3">
            <label className="block text-sm text-foreground">
              Name
              <input
                aria-label="Name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                className="mt-1 w-full rounded-md border border-border bg-background px-2 py-1.5 text-sm text-foreground"
              />
            </label>

            <div
              role="radiogroup"
              aria-label="Visibility"
              className="flex items-center gap-0.5 rounded-md border border-input p-0.5 text-xs"
            >
              <button
                type="button"
                role="radio"
                aria-checked={visibility === "org"}
                onClick={() => setVisibility("org")}
                className={`flex-1 rounded px-2 py-1 ${
                  visibility === "org"
                    ? "bg-accent text-foreground"
                    : "text-muted-foreground hover:text-foreground"
                }`}
              >
                Org
              </button>
              <button
                type="button"
                role="radio"
                aria-checked={visibility === "private"}
                onClick={() => setVisibility("private")}
                className={`flex-1 rounded px-2 py-1 ${
                  visibility === "private"
                    ? "bg-accent text-foreground"
                    : "text-muted-foreground hover:text-foreground"
                }`}
              >
                Private
              </button>
            </div>

            <label className="block text-sm text-foreground">
              Description
              <textarea
                aria-label="Description"
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                rows={3}
                className="mt-1 w-full rounded-md border border-border bg-background px-2 py-1.5 text-sm text-foreground"
              />
            </label>
          </div>

          {error && <p className="mt-2 text-sm text-destructive">{error}</p>}

          <div className="mt-4 flex justify-end gap-2">
            <Dialog.Close asChild>
              <button
                type="button"
                className="rounded-md border border-border px-3 py-1.5 text-sm font-medium text-foreground hover:bg-accent"
              >
                Cancel
              </button>
            </Dialog.Close>
            <button
              type="button"
              onClick={() => createMutation.mutate()}
              disabled={name.trim() === "" || createMutation.isPending}
              className="rounded-md bg-primary px-3 py-1.5 text-sm font-medium text-primary-foreground hover:opacity-90 disabled:opacity-50"
            >
              Create
            </button>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
