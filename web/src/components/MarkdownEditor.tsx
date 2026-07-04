import {
  MDXEditor,
  headingsPlugin,
  listsPlugin,
  quotePlugin,
  thematicBreakPlugin,
  linkPlugin,
  linkDialogPlugin,
  tablePlugin,
  codeBlockPlugin,
  codeMirrorPlugin,
  markdownShortcutPlugin,
  toolbarPlugin,
  UndoRedo,
  BoldItalicUnderlineToggles,
  BlockTypeSelect,
  ListsToggle,
  CreateLink,
  InsertTable,
  InsertThematicBreak,
} from "@mdxeditor/editor";
import "@mdxeditor/editor/style.css";

// codeMirrorPlugin needs a language map; "" is the fallback for fences with no info string.
const CODE_LANGS = {
  "": "Plain text",
  js: "JavaScript",
  ts: "TypeScript",
  tsx: "TSX",
  jsx: "JSX",
  go: "Go",
  bash: "Bash",
  sh: "Shell",
  json: "JSON",
  yaml: "YAML",
  yml: "YAML",
  md: "Markdown",
  py: "Python",
  sql: "SQL",
  html: "HTML",
  css: "CSS",
};

export default function MarkdownEditor({
  markdown,
  onChange,
  className,
}: {
  markdown: string;
  onChange: (md: string) => void;
  className?: string;
}) {
  // The app toggles `.dark` on <html>. MDXEditor themes via the `dark-theme` class.
  const dark = typeof document !== "undefined" && document.documentElement.classList.contains("dark");
  return (
    <MDXEditor
      markdown={markdown}
      onChange={onChange}
      className={`${dark ? "dark-theme " : ""}${className ?? ""}`}
      contentEditableClassName="prose prose-sm max-w-none"
      plugins={[
        headingsPlugin(),
        listsPlugin(),
        quotePlugin(),
        thematicBreakPlugin(),
        linkPlugin(),
        linkDialogPlugin(),
        tablePlugin(),
        codeBlockPlugin({ defaultCodeBlockLanguage: "" }),
        codeMirrorPlugin({ codeBlockLanguages: CODE_LANGS }),
        markdownShortcutPlugin(),
        toolbarPlugin({
          toolbarContents: () => (
            <>
              <UndoRedo />
              <BoldItalicUnderlineToggles />
              <BlockTypeSelect />
              <ListsToggle />
              <CreateLink />
              <InsertTable />
              <InsertThematicBreak />
            </>
          ),
        }),
      ]}
    />
  );
}
