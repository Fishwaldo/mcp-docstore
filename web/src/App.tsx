import { Routes, Route } from "react-router-dom";
import AppShell from "@/components/AppShell";
import AuthGate from "@/components/AuthGate";
import AuthCallback from "@/routes/AuthCallback";
import DocumentView from "@/routes/DocumentView";
import DiffView from "@/routes/DiffView";
import SearchResults from "@/routes/SearchResults";

export default function App() {
  return (
    <Routes>
      <Route path="/auth/callback" element={<AuthCallback />} />
      <Route element={<AuthGate />}>
        <Route element={<AppShell />}>
          <Route
            index
            element={
              <div className="p-8 text-muted-foreground">
                Select a document from the sidebar.
              </div>
            }
          />
          <Route path="documents/:id" element={<DocumentView />} />
          <Route path="documents/:id/diff" element={<DiffView />} />
          <Route path="search" element={<SearchResults />} />
        </Route>
      </Route>
    </Routes>
  );
}
