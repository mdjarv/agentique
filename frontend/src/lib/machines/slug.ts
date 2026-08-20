/**
 * Slug qualifier for remote machines' projects (multi-machine). Slugs are
 * unique per-server only — both machines can own a "agentique" project — so
 * remote projects get a stable machine-derived suffix at ingest and routes
 * stay slug-addressed with no machine segment. The primary machine's slugs
 * are never rewritten, so all existing URLs keep working.
 */
export function remoteSlug(slug: string, machineId: string): string {
  return `${slug}~${machineId.slice(0, 8)}`;
}
