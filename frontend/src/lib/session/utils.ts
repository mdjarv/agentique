import type { SessionMetadata } from "~/stores/chat-store";

export function findNearestActiveSession(
  sessions: Record<string, { meta: SessionMetadata }>,
  excludeId: string,
  projectId: string,
): string | null {
  let best: { id: string; createdAt: number } | null = null;
  for (const [id, data] of Object.entries(sessions)) {
    if (id === excludeId) continue;
    if (data.meta.projectId !== projectId) continue;
    if (data.meta.state !== "idle" && data.meta.state !== "running") continue;
    const t = new Date(data.meta.createdAt).getTime();
    if (!best || t > best.createdAt) {
      best = { id, createdAt: t };
    }
  }
  return best?.id ?? null;
}
