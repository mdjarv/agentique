export interface ClaudeAccount {
  loggedIn: boolean;
  email?: string;
  orgName?: string;
  subscriptionType?: string;
  authMethod?: string;
}

export async function getClaudeAccount(): Promise<ClaudeAccount> {
  const res = await fetch("/api/claude-account");
  if (!res.ok) return { loggedIn: false };
  return res.json();
}

export async function claudeLogout(): Promise<void> {
  const res = await fetch("/api/claude-account/logout", { method: "POST" });
  if (!res.ok) throw new Error("Logout failed");
}

export interface ClaudeLoginResult {
  status: string;
  url?: string;
}

export async function claudeLogin(): Promise<ClaudeLoginResult> {
  const res = await fetch("/api/claude-account/login", { method: "POST" });
  const data = await res.json();
  if (!res.ok) throw new Error(data?.error ?? "Login failed");
  return data;
}
