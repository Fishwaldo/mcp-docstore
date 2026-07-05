import { useState } from "react";
import { useParams, Link, useNavigate } from "react-router-dom";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import * as Dialog from "@radix-ui/react-dialog";
import {
  getProject,
  listDocuments,
  updateProject,
  archiveProject,
  unarchiveProject,
  deleteProject,
} from "@/lib/api";
import SharesPanel from "@/components/SharesPanel";

export default function ProjectView() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  const [editing, setEditing] = useState(false);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [saveError, setSaveError] = useState<string | null>(null);
  const [archiveError, setArchiveError] = useState<string | null>(null);
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [deleteConfirmText, setDeleteConfirmText] = useState("");
  const [deleteError, setDeleteError] = useState<string | null>(null);
  const [visibilityWarningOpen, setVisibilityWarningOpen] = useState(false);
  const [visibilityError, setVisibilityError] = useState<string | null>(null);

  const {
    data: project,
    isLoading: projectLoading,
    isError: projectError,
  } = useQuery({
    queryKey: ["project", id],
    queryFn: () => getProject(id!),
    enabled: !!id,
  });

  const {
    data: documents,
    isError: documentsError,
  } = useQuery({
    queryKey: ["documents", id],
    queryFn: () => listDocuments(id!),
    // Only fetch documents once the project has loaded — avoids a doomed second request
    // when getProject 404s (cross-tenant / no access).
    enabled: !!project,
  });

  const saveMutation = useMutation({
    mutationFn: (input: { name: string; description: string }) =>
      updateProject(id!, input),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["project", id] });
      queryClient.invalidateQueries({ queryKey: ["projects"] });
      setSaveError(null);
      setEditing(false);
    },
    onError: (err: unknown) => {
      setSaveError(err instanceof Error ? err.message : "Failed to save.");
    },
  });

  const archiveMutation = useMutation({
    mutationFn: () =>
      project?.archived ? unarchiveProject(id!) : archiveProject(id!),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["project", id] });
      queryClient.invalidateQueries({ queryKey: ["projects"] });
      setArchiveError(null);
    },
    onError: (err: unknown) => {
      setArchiveError(err instanceof Error ? err.message : "Failed to update archive status.");
    },
  });

  const deleteMutation = useMutation({
    mutationFn: () => deleteProject(id!),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["projects"] });
      setDeleteDialogOpen(false);
      navigate("/");
    },
    onError: (err: unknown) => {
      setDeleteError(err instanceof Error ? err.message : "Failed to delete project.");
    },
  });

  const visibilityMutation = useMutation({
    mutationFn: (visibility: "org" | "private") =>
      updateProject(id!, { visibility }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["project", id] });
      queryClient.invalidateQueries({ queryKey: ["projects"] });
      setVisibilityWarningOpen(false);
      setVisibilityError(null);
    },
    onError: (err: unknown) => {
      setVisibilityError(err instanceof Error ? err.message : "Failed to update visibility.");
    },
  });

  if (projectLoading) {
    return (
      <div className="p-8 space-y-4 animate-pulse">
        <div className="h-8 bg-muted rounded w-1/3" />
        <div className="h-4 bg-muted rounded w-full" />
        <div className="h-4 bg-muted rounded w-5/6" />
        <div className="h-4 bg-muted rounded w-4/5" />
      </div>
    );
  }

  if (projectError || !project) {
    return (
      <div className="p-8 text-destructive">
        Project not found.
      </div>
    );
  }

  const canManage = project.can_manage === true;

  function startEdit() {
    setName(project!.name);
    setDescription(project!.description);
    setSaveError(null);
    setEditing(true);
  }

  function cancelEdit() {
    setSaveError(null);
    setEditing(false);
  }

  function handleSave() {
    saveMutation.mutate({ name, description });
  }

  function openDeleteDialog() {
    setDeleteConfirmText("");
    setDeleteError(null);
    setDeleteDialogOpen(true);
  }

  function handleToggleVisibility() {
    setVisibilityError(null);
    if (project!.visibility === "org") {
      setVisibilityWarningOpen(true);
    } else {
      visibilityMutation.mutate("org");
    }
  }

  return (
    <div className="p-8 max-w-3xl">
      {editing ? (
        <div className="mb-4 space-y-3">
          <input
            aria-label="Name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            className="w-full rounded-md border border-border bg-background px-2 py-1.5 text-lg font-bold text-foreground"
          />
          <textarea
            aria-label="Description"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            rows={3}
            className="w-full rounded-md border border-border bg-background px-2 py-1.5 text-sm text-foreground"
          />
          {saveError && <p className="text-sm text-destructive">{saveError}</p>}
          <div className="flex gap-2">
            <button
              type="button"
              onClick={handleSave}
              disabled={saveMutation.isPending || name.trim() === ""}
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
      ) : (
        <>
          <h1 className="text-2xl font-bold text-foreground break-words mb-2">
            {project.name}
          </h1>
          {project.description && (
            <p className="text-sm text-muted-foreground mb-4 break-words">
              {project.description}
            </p>
          )}
        </>
      )}

      <div className={`flex flex-wrap gap-1.5 ${canManage && !editing ? "mb-3" : "mb-8"}`}>
        <span className="inline-flex items-center rounded-full border border-border px-2.5 py-0.5 text-xs font-medium text-foreground">
          {project.visibility === "org" ? "Org" : "Private"}
        </span>
        {project.access && (
          <span className="inline-flex items-center rounded-full border border-border px-2.5 py-0.5 text-xs font-medium text-foreground">
            {project.access}
          </span>
        )}
        {project.archived && (
          <span className="inline-flex items-center rounded-full border border-border px-2.5 py-0.5 text-xs font-medium text-foreground">
            Archived
          </span>
        )}
      </div>

      {canManage && !editing && (
        <div className="flex flex-wrap gap-2 mb-8">
          <button
            type="button"
            onClick={startEdit}
            className="inline-flex items-center justify-center rounded-md border border-border px-3 py-1.5 text-sm font-medium text-foreground hover:bg-accent"
          >
            Edit
          </button>
          <button
            type="button"
            onClick={() => archiveMutation.mutate()}
            disabled={archiveMutation.isPending}
            className="inline-flex items-center justify-center rounded-md border border-border px-3 py-1.5 text-sm font-medium text-foreground hover:bg-accent disabled:opacity-50"
          >
            {project.archived ? "Unarchive" : "Archive"}
          </button>
          <button
            type="button"
            onClick={handleToggleVisibility}
            disabled={visibilityMutation.isPending}
            className="inline-flex items-center justify-center rounded-md border border-border px-3 py-1.5 text-sm font-medium text-foreground hover:bg-accent disabled:opacity-50"
          >
            {project.visibility === "org" ? "Make private" : "Make org"}
          </button>
          <button
            type="button"
            onClick={openDeleteDialog}
            className="ml-auto inline-flex items-center justify-center rounded-md border border-destructive/50 px-3 py-1.5 text-sm font-medium text-destructive hover:bg-destructive/10"
          >
            Delete
          </button>
        </div>
      )}

      {archiveError && (
        <p className="mb-4 text-sm text-destructive">{archiveError}</p>
      )}
      {visibilityError && (
        <p className="mb-4 text-sm text-destructive">{visibilityError}</p>
      )}

      <h2 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-3">
        Documents
      </h2>
      {documentsError && (
        <p className="text-destructive">Couldn&rsquo;t load documents.</p>
      )}
      {!documentsError && documents && documents.length === 0 && (
        <p className="text-muted-foreground">No documents yet.</p>
      )}
      {documents && documents.length > 0 && (
        <ul className="space-y-4">
          {documents.map((doc) => (
            <li key={doc.id}>
              <Link
                to={`/documents/${doc.id}`}
                className="text-base font-medium text-primary hover:underline"
              >
                {doc.title}
              </Link>
              {doc.overview && (
                <p className="text-sm text-muted-foreground truncate">{doc.overview}</p>
              )}
            </li>
          ))}
        </ul>
      )}

      {canManage && project.visibility === "private" && (
        <div className="mt-8">
          <h2 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-3">
            Sharing
          </h2>
          <SharesPanel projectId={project.id} />
        </div>
      )}

      <Dialog.Root
        open={deleteDialogOpen}
        onOpenChange={(open) => {
          setDeleteDialogOpen(open);
          if (!open) setDeleteError(null);
        }}
      >
        <Dialog.Portal>
          <Dialog.Overlay className="fixed inset-0 bg-black/50" />
          <Dialog.Content className="fixed left-1/2 top-1/2 w-full max-w-sm -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-background p-6 shadow-lg">
            <Dialog.Title className="text-lg font-semibold text-foreground">
              Delete project?
            </Dialog.Title>
            <Dialog.Description className="mt-2 text-sm text-muted-foreground">
              This permanently deletes &ldquo;{project.name}&rdquo; and all of its documents.
              This action cannot be undone.
            </Dialog.Description>
            <label className="mt-4 block text-sm text-foreground">
              Type <span className="font-semibold">{project.name}</span> to confirm.
              <input
                aria-label="Project name"
                value={deleteConfirmText}
                onChange={(e) => setDeleteConfirmText(e.target.value)}
                className="mt-1 w-full rounded-md border border-border bg-background px-2 py-1.5 text-sm text-foreground"
              />
            </label>
            {deleteError && (
              <p className="mt-2 text-sm text-destructive">{deleteError}</p>
            )}
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
                onClick={() => deleteMutation.mutate()}
                disabled={deleteMutation.isPending || deleteConfirmText !== project.name}
                className="rounded-md bg-destructive px-3 py-1.5 text-sm font-medium text-destructive-foreground hover:opacity-90 disabled:opacity-50"
              >
                Yes, delete
              </button>
            </div>
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>

      <Dialog.Root open={visibilityWarningOpen} onOpenChange={setVisibilityWarningOpen}>
        <Dialog.Portal>
          <Dialog.Overlay className="fixed inset-0 bg-black/50" />
          <Dialog.Content className="fixed left-1/2 top-1/2 w-full max-w-sm -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-background p-6 shadow-lg">
            <Dialog.Title className="text-lg font-semibold text-foreground">
              Switch to private?
            </Dialog.Title>
            <Dialog.Description className="mt-2 text-sm text-muted-foreground">
              Switching to private revokes access for every tenant member who isn&rsquo;t the
              owner or an explicit share — continue?
            </Dialog.Description>
            {visibilityError && (
              <p className="mt-2 text-sm text-destructive">{visibilityError}</p>
            )}
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
                onClick={() => visibilityMutation.mutate("private")}
                disabled={visibilityMutation.isPending}
                className="rounded-md bg-primary px-3 py-1.5 text-sm font-medium text-primary-foreground hover:opacity-90 disabled:opacity-50"
              >
                Yes, make private
              </button>
            </div>
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>
    </div>
  );
}
