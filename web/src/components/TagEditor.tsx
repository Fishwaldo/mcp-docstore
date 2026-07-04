import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { X } from "lucide-react";
import { listTags } from "@/lib/api";

export default function TagEditor({
  tags,
  onChange,
}: {
  tags: string[];
  onChange: (tags: string[]) => void;
}) {
  const [input, setInput] = useState("");
  const { data: allTags } = useQuery({ queryKey: ["tags"], queryFn: listTags });

  const suggestions =
    input.length === 0
      ? []
      : (allTags ?? []).filter(
          (t) => !tags.includes(t) && t.toLowerCase().includes(input.toLowerCase())
        );

  function addTag(raw: string) {
    const trimmed = raw.trim();
    if (!trimmed || tags.includes(trimmed)) return;
    onChange([...tags, trimmed]);
    setInput("");
  }

  function removeTag(tag: string) {
    onChange(tags.filter((t) => t !== tag));
  }

  function handleKeyDown(e: React.KeyboardEvent<HTMLInputElement>) {
    if (e.key === "Enter" || e.key === ",") {
      e.preventDefault();
      addTag(input);
    }
  }

  return (
    <div>
      <div className="flex flex-wrap gap-1.5 mb-2">
        {tags.map((tag) => (
          <span
            key={tag}
            className="inline-flex items-center gap-1 rounded-full border border-border px-2.5 py-0.5 text-xs font-medium text-foreground"
          >
            {tag}
            <button
              type="button"
              onClick={() => removeTag(tag)}
              aria-label={`Remove ${tag}`}
              className="text-muted-foreground hover:text-foreground"
            >
              <X className="h-3 w-3" />
            </button>
          </span>
        ))}
      </div>
      <input
        type="text"
        value={input}
        onChange={(e) => setInput(e.target.value)}
        onKeyDown={handleKeyDown}
        placeholder="Add tag…"
        className="w-full rounded-md border border-border bg-background px-2 py-1 text-sm text-foreground"
      />
      {suggestions.length > 0 && (
        <ul className="mt-1 rounded-md border border-border bg-popover shadow-sm">
          {suggestions.map((tag) => (
            <li key={tag}>
              <button
                type="button"
                onClick={() => addTag(tag)}
                className="block w-full px-2 py-1 text-left text-sm text-foreground hover:bg-accent"
              >
                {tag}
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
