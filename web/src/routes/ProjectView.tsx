import { useParams, Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { getProject, listDocuments } from "@/lib/api";

export default function ProjectView() {
  const { id } = useParams<{ id: string }>();

  const {
    data: project,
    isLoading: projectLoading,
    isError: projectError,
  } = useQuery({
    queryKey: ["project", id],
    queryFn: () => getProject(id!),
    enabled: !!id,
  });

  const { data: documents } = useQuery({
    queryKey: ["documents", id],
    queryFn: () => listDocuments(id!),
    enabled: !!id,
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

  return (
    <div className="p-8 max-w-3xl">
      <h1 className="text-2xl font-bold text-foreground break-words mb-2">
        {project.name}
      </h1>
      {project.description && (
        <p className="text-sm text-muted-foreground mb-4 break-words">
          {project.description}
        </p>
      )}

      <div className="flex flex-wrap gap-1.5 mb-8">
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

      <h2 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-3">
        Documents
      </h2>
      {documents && documents.length === 0 && (
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
