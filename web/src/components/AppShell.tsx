import { useEffect, useState } from "react";
import { Outlet } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { ChevronLeft, ChevronRight, LogOut, Moon, Sun } from "lucide-react";
import ProjectTree from "@/components/ProjectTree";
import NoAccessScreen from "@/components/NoAccessScreen";
import { getMe, NO_ACCESS_EVENT } from "@/lib/api";
import { login, logout } from "@/lib/oauth";
import { useTheme } from "@/hooks/useTheme";

export default function AppShell() {
  const [collapsed, setCollapsed] = useState(false);
  const [noAccess, setNoAccess] = useState(false);
  const [signedOut, setSignedOut] = useState(false);
  const { theme, toggle } = useTheme();

  useEffect(() => {
    function onNoAccess() {
      setNoAccess(true);
    }
    window.addEventListener(NO_ACCESS_EVENT, onNoAccess);
    return () => window.removeEventListener(NO_ACCESS_EVENT, onNoAccess);
  }, []);

  const { data: me } = useQuery({
    queryKey: ["me"],
    queryFn: getMe,
    enabled: !noAccess && !signedOut,
    retry: false,
  });

  async function handleLogout() {
    await logout();
    setSignedOut(true);
  }

  if (noAccess) {
    return <NoAccessScreen />;
  }

  if (signedOut) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-background">
        <div className="max-w-sm space-y-4 text-center">
          <p className="text-lg font-medium text-foreground">Signed out of DocStore</p>
          <p className="text-sm text-muted-foreground">
            Your upstream identity provider session may still be active, so clicking Login below
            can re-authenticate you without asking for credentials again.
          </p>
          <button
            onClick={() => void login("/")}
            className="inline-flex items-center justify-center rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:opacity-90"
          >
            Login
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="flex min-h-screen bg-background">
      {/* Sidebar */}
      <aside
        className="flex flex-col border-r border-border bg-muted/30 transition-all duration-200"
        style={{ width: collapsed ? 48 : 280 }}
      >
        {/* Sidebar header */}
        <div className="flex h-12 items-center justify-between px-3 border-b border-border shrink-0">
          <div className="flex items-center gap-2 min-w-0">
            <img src="/icon-96.png" alt="DocStore" className="h-6 w-6 rounded shrink-0" />
            {!collapsed && (
              <span className="text-sm font-semibold text-foreground truncate">DocStore</span>
            )}
          </div>
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

        {/* Identity + logout */}
        <div className="border-t border-border p-3 shrink-0">
          {!collapsed && me && (
            <p className="mb-1.5 truncate text-xs text-muted-foreground" title={me.email}>
              {me.email}
            </p>
          )}
          <button
            onClick={toggle}
            className="flex w-full items-center gap-1.5 rounded-md px-2 py-1.5 text-sm text-foreground hover:bg-accent"
            aria-label={theme === "dark" ? "Switch to light mode" : "Switch to dark mode"}
          >
            {theme === "dark" ? (
              <Sun className="h-3.5 w-3.5 shrink-0" />
            ) : (
              <Moon className="h-3.5 w-3.5 shrink-0" />
            )}
            {!collapsed && <span>{theme === "dark" ? "Light mode" : "Dark mode"}</span>}
          </button>
          <button
            onClick={() => void handleLogout()}
            className="flex w-full items-center gap-1.5 rounded-md px-2 py-1.5 text-sm text-foreground hover:bg-accent"
            aria-label="Logout"
          >
            <LogOut className="h-3.5 w-3.5 shrink-0" />
            {!collapsed && <span>Logout</span>}
          </button>
        </div>
      </aside>

      {/* Main content */}
      <main className="flex-1 overflow-y-auto">
        <Outlet />
      </main>
    </div>
  );
}
