import { useMemo, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight, ChevronDown, Folder, FolderOpen, Plus } from "lucide-react";
import {
  listProjects,
  listDocuments,
  searchDocuments,
  type DocumentSummaryDTO,
  type SearchHitDTO,
  type ProjectDTO,
} from "@/lib/api";
import TagFilter from "@/components/TagFilter";
import NewProjectDialog from "@/components/NewProjectDialog";

type DocOrder = "title" | "recent";

const DOC_ORDER_KEY = "docOrder";

function getInitialDocOrder(): DocOrder {
  const stored = localStorage.getItem(DOC_ORDER_KEY);
  return stored === "recent" ? "recent" : "title";
}

function sortDocuments(
  documents: DocumentSummaryDTO[],
  order: DocOrder,
): DocumentSummaryDTO[] {
  return order === "recent"
    ? [...documents].sort((a, b) => b.updated_at.localeCompare(a.updated_at))
    : [...documents].sort((a, b) => a.title.localeCompare(b.title));
}

function ProjectItem({
  projectId,
  projectName,
  order,
  archived = false,
}: {
  projectId: string;
  projectName: string;
  order: DocOrder;
  archived?: boolean;
}) {
  const [expanded, setExpanded] = useState(false);

  const { data: documents, isLoading } = useQuery({
    queryKey: ["documents", projectId],
    queryFn: () => listDocuments(projectId),
    enabled: expanded,
  });

  const sortedDocuments = useMemo(
    () => (documents ? sortDocuments(documents, order) : documents),
    [documents, order],
  );

  return (
    <div className={archived ? "opacity-60" : undefined}>
      <div className="flex w-full items-center gap-1.5 rounded-md hover:bg-accent">
        <button
          type="button"
          aria-label={`Toggle ${projectName}`}
          className="flex shrink-0 items-center px-2 py-1.5 text-muted-foreground"
          onClick={() => setExpanded((e) => !e)}
        >
          {expanded ? (
            <ChevronDown className="h-3.5 w-3.5 shrink-0" />
          ) : (
            <ChevronRight className="h-3.5 w-3.5 shrink-0" />
          )}
        </button>
        <Link
          to={`/projects/${projectId}`}
          className="flex min-w-0 flex-1 items-center gap-1.5 py-1.5 pr-2 text-sm text-foreground"
        >
          {expanded ? (
            <FolderOpen className="h-4 w-4 shrink-0 text-muted-foreground" />
          ) : (
            <Folder className="h-4 w-4 shrink-0 text-muted-foreground" />
          )}
          <span className="truncate">{projectName}</span>
        </Link>
      </div>

      {expanded && (
        <div className="ml-7 mt-0.5 space-y-0.5">
          {isLoading && (
            <div className="px-2 py-1 text-xs text-muted-foreground">Loading…</div>
          )}
          {sortedDocuments?.map((doc) => (
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

function FilteredProjectItem({
  project,
  hits,
}: {
  project: ProjectDTO;
  hits: SearchHitDTO[];
}) {
  return (
    <div>
      <div className="flex w-full items-center gap-1.5 rounded-md hover:bg-accent">
        <div className="flex shrink-0 items-center px-2 py-1.5 text-muted-foreground">
          <ChevronDown className="h-3.5 w-3.5 shrink-0" />
        </div>
        <Link
          to={`/projects/${project.id}`}
          className="flex min-w-0 flex-1 items-center gap-1.5 py-1.5 pr-2 text-sm text-foreground"
        >
          <FolderOpen className="h-4 w-4 shrink-0 text-muted-foreground" />
          <span className="truncate">{project.name}</span>
        </Link>
      </div>
      <div className="ml-7 mt-0.5 space-y-0.5">
        {hits.map((hit) => (
          <Link
            key={hit.document_id}
            to={`/documents/${hit.document_id}`}
            className="block px-2 py-1 text-sm text-foreground hover:bg-accent rounded-md truncate"
          >
            {hit.title}
          </Link>
        ))}
      </div>
    </div>
  );
}

export default function ProjectTree() {
  const navigate = useNavigate();
  const [searchValue, setSearchValue] = useState("");
  const [order, setOrder] = useState<DocOrder>(() => getInitialDocOrder());
  const [activeTags, setActiveTags] = useState<string[]>([]);
  const [showArchived, setShowArchived] = useState(false);

  const { data: projects, isLoading, isError } = useQuery({
    queryKey: ["projects", showArchived],
    queryFn: () => listProjects(showArchived),
  });

  const {
    data: filteredHits,
    isLoading: filteredLoading,
    isError: filteredError,
  } = useQuery({
    queryKey: ["docsByTags", activeTags],
    queryFn: () => searchDocuments({ q: "", tags: activeTags }),
    enabled: activeTags.length > 0,
  });

  function handleSearch(e: React.FormEvent) {
    e.preventDefault();
    if (searchValue.trim()) {
      navigate(`/search?q=${encodeURIComponent(searchValue.trim())}`);
    }
  }

  function handleOrderChange(next: DocOrder) {
    setOrder(next);
    localStorage.setItem(DOC_ORDER_KEY, next);
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
        <NewProjectDialog />
      </div>

      <button
        type="button"
        aria-pressed={showArchived}
        onClick={() => setShowArchived((v) => !v)}
        className={`self-start rounded-md border border-input px-2 py-1 text-xs ${
          showArchived
            ? "bg-accent text-foreground"
            : "text-muted-foreground hover:text-foreground"
        }`}
      >
        Show archived
      </button>

      <div
        role="radiogroup"
        aria-label="Document order"
        className="flex items-center gap-0.5 rounded-md border border-input p-0.5 text-xs"
      >
        <button
          type="button"
          role="radio"
          onClick={() => handleOrderChange("title")}
          aria-checked={order === "title"}
          className={`flex-1 rounded px-2 py-1 ${
            order === "title"
              ? "bg-accent text-foreground"
              : "text-muted-foreground hover:text-foreground"
          }`}
        >
          A–Z
        </button>
        <button
          type="button"
          role="radio"
          onClick={() => handleOrderChange("recent")}
          aria-checked={order === "recent"}
          className={`flex-1 rounded px-2 py-1 ${
            order === "recent"
              ? "bg-accent text-foreground"
              : "text-muted-foreground hover:text-foreground"
          }`}
        >
          Recent
        </button>
      </div>

      <TagFilter selected={activeTags} onChange={setActiveTags} />

      {activeTags.length === 0 ? (
        <div className="space-y-0.5">
          {isLoading && (
            <div className="px-2 py-2 text-xs text-muted-foreground">Loading projects…</div>
          )}
          {isError && (
            <div className="px-2 py-2 text-xs text-destructive">Failed to load projects</div>
          )}
          {projects?.map((project) => (
            <ProjectItem
              key={project.id}
              projectId={project.id}
              projectName={project.name}
              order={order}
              archived={project.archived}
            />
          ))}
        </div>
      ) : (
        <div className="space-y-0.5">
          {filteredLoading && (
            <div className="px-2 py-2 text-xs text-muted-foreground">Filtering…</div>
          )}
          {filteredError && (
            <div className="px-2 py-2 text-xs text-destructive">Couldn&rsquo;t filter documents.</div>
          )}
          {!filteredLoading && !filteredError && filteredHits?.length === 0 && (
            <div className="px-2 py-2 text-xs text-muted-foreground">No documents match.</div>
          )}
          {!filteredLoading &&
            filteredHits &&
            filteredHits.length > 0 &&
            (() => {
              const hitsByProject = new Map<string, SearchHitDTO[]>();
              for (const hit of filteredHits) {
                const existing = hitsByProject.get(hit.project_id);
                if (existing) {
                  existing.push(hit);
                } else {
                  hitsByProject.set(hit.project_id, [hit]);
                }
              }
              return projects
                ?.filter((project) => hitsByProject.has(project.id))
                .map((project) => (
                  <FilteredProjectItem
                    key={project.id}
                    project={project}
                    hits={hitsByProject.get(project.id)!}
                  />
                ));
            })()}
        </div>
      )}
    </div>
  );
}
