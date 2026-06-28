import { useState } from "react";
import { Outlet } from "react-router-dom";
import { ChevronLeft, ChevronRight } from "lucide-react";
import ProjectTree from "@/components/ProjectTree";

export default function AppShell() {
  const [collapsed, setCollapsed] = useState(false);

  return (
    <div className="flex min-h-screen bg-background">
      {/* Sidebar */}
      <aside
        className="flex flex-col border-r border-border bg-muted/30 transition-all duration-200"
        style={{ width: collapsed ? 48 : 280 }}
      >
        {/* Sidebar header */}
        <div className="flex h-12 items-center justify-between px-3 border-b border-border shrink-0">
          {!collapsed && (
            <span className="text-sm font-semibold text-foreground truncate">DocStore</span>
          )}
          <button
            onClick={() => setCollapsed((c) => !c)}
            className="ml-auto flex h-7 w-7 items-center justify-center rounded-md hover:bg-accent text-muted-foreground"
            aria-label={collapsed ? "Expand sidebar" : "Collapse sidebar"}
          >
            {collapsed ? (
              <ChevronRight className="h-4 w-4" />
            ) : (
              <ChevronLeft className="h-4 w-4" />
            )}
          </button>
        </div>

        {/* Tree content */}
        {!collapsed && (
          <div className="flex-1 overflow-y-auto p-3">
            <ProjectTree />
          </div>
        )}
      </aside>

      {/* Main content */}
      <main className="flex-1 overflow-y-auto">
        <Outlet />
      </main>
    </div>
  );
}
