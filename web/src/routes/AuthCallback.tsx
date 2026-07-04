import { useEffect, useState } from "react";
import { handleCallback } from "@/lib/oauth";

// AuthCallback is the /auth/callback landing page: handleCallback() validates the PKCE state,
// exchanges the code, and navigates away to the original returnTo on success (a full navigation,
// not a router push — see oauth.ts). This component only ever renders while that exchange is in
// flight, or if it fails before navigating away.
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
