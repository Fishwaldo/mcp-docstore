import { Routes, Route, Outlet, Link } from "react-router-dom";

function Shell() {
  return (
    <div className="min-h-screen bg-background">
      <header className="border-b border-border px-6 py-3 flex items-center gap-4">
        <Link to="/" className="text-xl font-semibold text-foreground">
          DocStore
        </Link>
      </header>
      <main className="container mx-auto px-6 py-8">
        <Outlet />
      </main>
    </div>
  );
}

function ProjectsPage() {
  return <div>Projects — coming in Task 2</div>;
}

function ProjectPage() {
  return <div>Project — coming in Task 2</div>;
}

function DocumentPage() {
  return <div>Document — coming in Task 2</div>;
}

function SearchPage() {
  return <div>Search — coming in Task 2</div>;
}

export default function App() {
  return (
    <Routes>
      <Route element={<Shell />}>
        <Route index element={<ProjectsPage />} />
        <Route path="projects/:id" element={<ProjectPage />} />
        <Route path="documents/:id" element={<DocumentPage />} />
        <Route path="search" element={<SearchPage />} />
      </Route>
    </Routes>
  );
}
