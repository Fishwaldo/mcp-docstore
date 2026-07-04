import { useEffect, useState } from "react";
import { Outlet } from "react-router-dom";
import { isAuthenticated, login } from "@/lib/oauth";

// AuthGate wraps every route except /auth/callback: if this tab has no way to produce an
// access token (no in-memory token, no refresh token in sessionStorage), it starts a fresh
// login immediately rather than letting a protected route render and fail its API calls one by
// one. It renders nothing of the app until that check has resolved one way or the other.
export default function AuthGate() {
  const [authenticated, setAuthenticated] = useState(isAuthenticated());

  useEffect(() => {
    if (isAuthenticated()) {
      setAuthenticated(true);
      return;
    }
    void login(window.location.pathname + window.location.search);
  }, []);

  if (!authenticated) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-background">
        <p className="text-muted-foreground">Redirecting to sign in…</p>
      </div>
    );
  }

  return <Outlet />;
}
