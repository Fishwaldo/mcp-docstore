import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { listProjects, createDocument } from "@/lib/api";
import MarkdownEditor from "@/components/MarkdownEditor";
import TagEditor from "@/components/TagEditor";

export default function NewDocument() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  const [projectId, setProjectId] = useState("");
  const [title, setTitle] = useState("");
  const [overview, setOverview] = useState("");
  const [body, setBody] = useState("");
  const [tags, setTags] = useState<string[]>([]);

  const { data: projects, isLoading: projectsLoading } = useQuery({
    queryKey: ["projects"],
    queryFn: () => listProjects(),
  });

  const writableProjects = (projects ?? []).filter((p) => p.access === "write");
  const selectedProjectId = projectId || writableProjects[0]?.id || "";

  const createMutation = useMutation({
    mutationFn: () =>
      createDocument({
        project_id: selectedProjectId,
        title,
        overview: overview || undefined,
        body: body || undefined,
        tags,
      }),
    onSuccess: (created) => {
      queryClient.invalidateQueries({ queryKey: ["documents", selectedProjectId] });
      queryClient.invalidateQueries({ queryKey: ["docsByTags"] });
      navigate(`/documents/${created.id}`);
    },
  });

  const canSubmit = !!selectedProjectId && title.trim().length > 0 && !createMutation.isPending;

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!canSubmit) return;
    createMutation.mutate();
  }

  return (
    <div className="max-w-3xl p-8">
      <h1 className="text-2xl font-bold text-foreground mb-6">New document</h1>

      <form onSubmit={handleSubmit} className="space-y-6">
        <div>
          <label htmlFor="new-doc-project" className="block text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-2">
            Project
          </label>
          {projectsLoading ? (
            <p className="text-sm text-muted-foreground">Loading projects…</p>
          ) : writableProjects.length === 0 ? (
            <p className="text-sm text-destructive">
              No projects with write access are available.
            </p>
          ) : (
            <select
              id="new-doc-project"
              value={selectedProjectId}
              onChange={(e) => setProjectId(e.target.value)}
              className="w-full rounded-md border border-border bg-background px-2 py-1.5 text-sm text-foreground"
            >
              {writableProjects.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </select>
          )}
        </div>

        <div>
          <label htmlFor="new-doc-title" className="block text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-2">
            Title
          </label>
          <input
            id="new-doc-title"
            type="text"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            className="w-full rounded-md border border-border bg-background px-2 py-1.5 text-sm text-foreground"
          />
        </div>

        <div>
          <label htmlFor="new-doc-overview" className="block text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-2">
            Overview
          </label>
          <textarea
            id="new-doc-overview"
            value={overview}
            onChange={(e) => setOverview(e.target.value)}
            rows={3}
            className="w-full rounded-md border border-border bg-background px-2 py-1.5 text-sm text-foreground"
          />
        </div>

        <div>
          <h2 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-2">
            Body
          </h2>
          <MarkdownEditor markdown={body} onChange={setBody} />
        </div>

        <div>
          <h2 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-2">
            Tags
          </h2>
          <TagEditor tags={tags} onChange={setTags} />
        </div>

        {createMutation.isError && (
          <p className="text-sm text-destructive">
            {createMutation.error instanceof Error
              ? createMutation.error.message
              : "Failed to create document."}
          </p>
        )}

        <div className="flex gap-2">
          <button
            type="submit"
            disabled={!canSubmit}
            className="rounded-md bg-primary px-3 py-1.5 text-sm font-medium text-primary-foreground hover:opacity-90 disabled:opacity-50"
          >
            Create
          </button>
        </div>
      </form>
    </div>
  );
}
