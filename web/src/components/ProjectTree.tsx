import { useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight, ChevronDown, Folder, FolderOpen, Plus } from "lucide-react";
import { listProjects, listDocuments } from "@/lib/api";

function ProjectItem({ projectId, projectName }: { projectId: string; projectName: string }) {
  const [expanded, setExpanded] = useState(false);

  const { data: documents, isLoading } = useQuery({
    queryKey: ["documents", projectId],
    queryFn: () => listDocuments(projectId),
    enabled: expanded,
  });

  return (
    <div>
      <button
        className="flex w-full items-center gap-1.5 px-2 py-1.5 text-sm text-foreground hover:bg-accent rounded-md"
        onClick={() => setExpanded((e) => !e)}
      >
        {expanded ? (
          <ChevronDown className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        ) : (
          <ChevronRight className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        )}
        {expanded ? (
          <FolderOpen className="h-4 w-4 shrink-0 text-muted-foreground" />
        ) : (
          <Folder className="h-4 w-4 shrink-0 text-muted-foreground" />
        )}
        <span className="truncate">{projectName}</span>
      </button>

      {expanded && (
        <div className="ml-7 mt-0.5 space-y-0.5">
          {isLoading && (
            <div className="px-2 py-1 text-xs text-muted-foreground">Loading…</div>
          )}
          {documents?.map((doc) => (
            <Link
              key={doc.id}
              to={`/documents/${doc.id}`}
              className="block px-2 py-1 text-sm text-foreground hover:bg-accent rounded-md truncate"
            >
              {doc.title}
            </Link>
          ))}
          {documents?.length === 0 && (
            <div className="px-2 py-1 text-xs text-muted-foreground">No documents</div>
          )}
        </div>
      )}
    </div>
  );
}

export default function ProjectTree() {
  const navigate = useNavigate();
  const [searchValue, setSearchValue] = useState("");

  const { data: projects, isLoading, isError } = useQuery({
    queryKey: ["projects"],
    queryFn: () => listProjects(),
  });

  function handleSearch(e: React.FormEvent) {
    e.preventDefault();
    if (searchValue.trim()) {
      navigate(`/search?q=${encodeURIComponent(searchValue.trim())}`);
    }
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center gap-2">
        <form onSubmit={handleSearch} className="flex-1">
          <input
            type="search"
            value={searchValue}
            onChange={(e) => setSearchValue(e.target.value)}
            placeholder="Search docs…"
            className="w-full rounded-md border border-input bg-background px-3 py-1.5 text-sm placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-ring"
          />
        </form>
        <Link
          to="/documents/new"
          aria-label="New document"
          title="New document"
          className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md border border-border text-muted-foreground hover:bg-accent hover:text-foreground"
        >
          <Plus className="h-4 w-4" />
        </Link>
      </div>

      <div className="space-y-0.5">
        {isLoading && (
          <div className="px-2 py-2 text-xs text-muted-foreground">Loading projects…</div>
        )}
        {isError && (
          <div className="px-2 py-2 text-xs text-destructive">Failed to load projects</div>
        )}
        {projects?.map((project) => (
          <ProjectItem key={project.id} projectId={project.id} projectName={project.name} />
        ))}
      </div>
    </div>
  );
}
