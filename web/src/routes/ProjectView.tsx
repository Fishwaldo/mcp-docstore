import { useState } from "react";
import { useParams, Link } from "react-router-dom";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  getProject,
  listDocuments,
  updateProject,
  archiveProject,
  unarchiveProject,
} from "@/lib/api";

export default function ProjectView() {
  const { id } = useParams<{ id: string }>();
  const queryClient = useQueryClient();

  const [editing, setEditing] = useState(false);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [saveError, setSaveError] = useState<string | null>(null);

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
        <div className="flex gap-2 mb-8">
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
        </div>
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
    </div>
  );
}
