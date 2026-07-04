// NoAccessScreen renders when a request comes back 403 {"error":"no_access"}: the caller
// authenticated successfully but isn't provisioned for any tenant. This is deliberately NOT a
// login redirect — the user already has a valid token, so sending them through /oauth/authorize
// again would just hand back another token for the same unprovisioned account and loop forever.
export default function NoAccessScreen() {
  return (
    <div className="flex min-h-screen items-center justify-center bg-background">
      <div className="max-w-sm text-center space-y-2">
        <img src="/icon-96.png" alt="DocStore" className="mx-auto h-12 w-12 rounded" />
        <p className="text-lg font-medium text-foreground">No access</p>
        <p className="text-sm text-muted-foreground">
          You&rsquo;re signed in, but your account isn&rsquo;t provisioned for any tenant in
          DocStore. Ask an administrator to grant you access, then reload this page.
        </p>
      </div>
    </div>
  );
}
