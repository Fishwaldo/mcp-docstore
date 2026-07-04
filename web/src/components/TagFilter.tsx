import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronDown } from "lucide-react";
import { listTags } from "@/lib/api";

export default function TagFilter({
  selected,
  onChange,
}: {
  selected: string[];
  onChange: (tags: string[]) => void;
}) {
  const [open, setOpen] = useState(false);

  const { data: tags } = useQuery({
    queryKey: ["tags"],
    queryFn: () => listTags(),
  });

  function toggleTag(tag: string) {
    onChange(
      selected.includes(tag) ? selected.filter((t) => t !== tag) : [...selected, tag],
    );
  }

  return (
    <div>
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        className="flex w-full items-center justify-between gap-2 px-2 py-1 text-xs font-semibold text-muted-foreground uppercase tracking-wider hover:text-foreground"
      >
        <span>Filter by tag</span>
        <ChevronDown
          className={`h-3.5 w-3.5 shrink-0 transition-transform ${open ? "" : "-rotate-90"}`}
        />
      </button>

      {open && (
        <div className="space-y-1 px-2 py-1">
          {selected.length > 0 && (
            <button
              type="button"
              onClick={() => onChange([])}
              className="text-xs text-primary hover:underline"
            >
              Clear
            </button>
          )}
          {tags?.map((tag) => (
            <label
              key={tag}
              className="flex items-center gap-2 text-sm text-foreground"
            >
              <input
                type="checkbox"
                checked={selected.includes(tag)}
                onChange={() => toggleTag(tag)}
              />
              {tag}
            </label>
          ))}
          {tags?.length === 0 && (
            <p className="text-xs text-muted-foreground">No tags yet</p>
          )}
        </div>
      )}
    </div>
  );
}
