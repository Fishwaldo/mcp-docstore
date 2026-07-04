import { useEffect, useState } from "react";
import { handleCallback, login } from "@/lib/oauth";

// AuthCallback is the /auth/callback landing page: handleCallback() validates the PKCE state,
// exchanges the code, and navigates away to the original returnTo on success (a full navigation,
// not a router push — see oauth.ts). On failure it throws, and this view renders a terminal
// error with a user-initiated retry. It deliberately does NOT auto-restart login: a provider
// that keeps denying would otherwise loop authorize→callback→login indefinitely.
export default function AuthCallback() {
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    handleCallback().catch((err: unknown) => {
      setError(err instanceof Error ? err.message : "Sign-in failed.");
    });
  }, []);

  if (error) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-background">
        <div className="max-w-sm text-center">
          <p className="text-destructive font-medium">Sign-in failed.</p>
          <p className="mt-2 text-sm text-muted-foreground">{error}</p>
          <button
            type="button"
            className="mt-4 inline-flex h-9 items-center justify-center rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground hover:bg-primary/90"
            onClick={() => void login("/")}
          >
            Try again
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-background">
      <p className="text-muted-foreground">Signing in…</p>
    </div>
  );
}
