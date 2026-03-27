import { browserSupportsWebAuthn } from "@simplewebauthn/browser";
import { useState } from "react";
import { Button } from "~/components/ui/button";
import { Input } from "~/components/ui/input";
import { Label } from "~/components/ui/label";
import { login, register } from "~/lib/auth-api";
import { getErrorMessage } from "~/lib/utils";
import { useAuthStore } from "~/stores/auth-store";

export function LoginPage() {
  const userCount = useAuthStore((s) => s.userCount);
  const isFirstUser = userCount === 0;

  if (!browserSupportsWebAuthn()) {
    return (
      <CenterLayout>
        <p className="text-destructive">This browser does not support passkeys (WebAuthn).</p>
      </CenterLayout>
    );
  }

  return <CenterLayout>{isFirstUser ? <SetupForm /> : <LoginForm />}</CenterLayout>;
}

function SetupForm() {
  const setAuthenticated = useAuthStore((s) => s.setAuthenticated);
  const [displayName, setDisplayName] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  async function handleRegister(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    if (!displayName.trim()) return;

    setError("");
    setLoading(true);
    try {
      const user = await register(displayName.trim());
      setAuthenticated(user);
    } catch (err) {
      setError(getErrorMessage(err, "Registration failed"));
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="space-y-6">
      <div className="space-y-2 text-center">
        <h1 className="text-2xl font-semibold tracking-tight">Set up Agentique</h1>
        <p className="text-sm text-muted-foreground">Create a passkey to secure your instance.</p>
      </div>
      <form onSubmit={handleRegister} className="space-y-4">
        <div className="space-y-2">
          <Label htmlFor="displayName">Display name</Label>
          <Input
            id="displayName"
            value={displayName}
            onChange={(e) => setDisplayName(e.target.value)}
            placeholder="Your name"
            autoFocus
            disabled={loading}
          />
        </div>
        <Button type="submit" className="w-full" disabled={loading || !displayName.trim()}>
          {loading ? "Creating passkey..." : "Register with passkey"}
        </Button>
      </form>
      {error && <p className="text-sm text-destructive text-center">{error}</p>}
    </div>
  );
}

function LoginForm() {
  const setAuthenticated = useAuthStore((s) => s.setAuthenticated);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  async function handleLogin() {
    setError("");
    setLoading(true);
    try {
      const user = await login();
      setAuthenticated(user);
    } catch (err) {
      setError(getErrorMessage(err, "Login failed"));
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="space-y-6">
      <div className="space-y-2 text-center">
        <h1 className="text-2xl font-semibold tracking-tight">Agentique</h1>
        <p className="text-sm text-muted-foreground">Sign in with your passkey.</p>
      </div>
      <Button onClick={handleLogin} className="w-full" disabled={loading}>
        {loading ? "Authenticating..." : "Sign in with passkey"}
      </Button>
      {error && <p className="text-sm text-destructive text-center">{error}</p>}
    </div>
  );
}

function CenterLayout({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex h-screen items-center justify-center bg-background">
      <div className="w-full max-w-sm px-4">{children}</div>
    </div>
  );
}
