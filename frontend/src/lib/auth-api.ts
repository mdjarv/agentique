import { startAuthentication, startRegistration } from "@simplewebauthn/browser";

const BASE = "/api/auth";

export interface AuthUser {
  id: string;
  displayName: string;
  isAdmin: boolean;
}

export interface AuthStatus {
  authEnabled: boolean;
  authenticated: boolean;
  userCount: number;
  user?: AuthUser;
}

export async function getAuthStatus(): Promise<AuthStatus> {
  const res = await fetch(`${BASE}/status`);
  if (!res.ok) throw new Error("Failed to get auth status");
  return res.json();
}

export async function register(displayName: string, inviteToken?: string): Promise<AuthUser> {
  // Step 1: Begin registration — get WebAuthn options from server.
  const beginRes = await fetch(`${BASE}/register/begin`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ displayName, inviteToken }),
  });
  if (!beginRes.ok) {
    const err = await beginRes.json().catch(() => null);
    throw new Error(err?.error ?? "Registration failed");
  }
  const beginData = await beginRes.json();

  // Step 2: Create passkey via browser API.
  const credential = await startRegistration({ optionsJSON: beginData.options.publicKey });

  // Step 3: Send credential back to server.
  const params = new URLSearchParams({ ceremonyKey: beginData.ceremonyKey });
  if (beginData.inviteToken) {
    params.set("inviteToken", beginData.inviteToken);
  }
  const finishRes = await fetch(`${BASE}/register/finish?${params}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(credential),
  });
  if (!finishRes.ok) {
    const err = await finishRes.json().catch(() => null);
    throw new Error(err?.error ?? "Registration verification failed");
  }

  const result = await finishRes.json();
  return result.user;
}

export async function login(): Promise<AuthUser> {
  // Step 1: Begin login — get WebAuthn options from server.
  const beginRes = await fetch(`${BASE}/login/begin`, { method: "POST" });
  if (!beginRes.ok) throw new Error("Login failed");
  const beginData = await beginRes.json();

  // Step 2: Authenticate via browser API.
  const credential = await startAuthentication({ optionsJSON: beginData.options.publicKey });

  // Step 3: Send assertion back to server.
  const params = new URLSearchParams({ ceremonyKey: beginData.ceremonyKey });
  const finishRes = await fetch(`${BASE}/login/finish?${params}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(credential),
  });
  if (!finishRes.ok) {
    const err = await finishRes.json().catch(() => null);
    throw new Error(err?.error ?? "Login verification failed");
  }

  const result = await finishRes.json();
  return result.user;
}

export async function logout(): Promise<void> {
  await fetch(`${BASE}/logout`, { method: "POST" });
}

export async function createInvite(): Promise<{ token: string; expiresAt: string }> {
  const res = await fetch(`${BASE}/invite`, { method: "POST" });
  if (!res.ok) throw new Error("Failed to create invite");
  return res.json();
}

export async function validateInvite(token: string): Promise<boolean> {
  const res = await fetch(`${BASE}/invite/${token}`);
  if (!res.ok) return false;
  const data = await res.json();
  return data.valid === true;
}
