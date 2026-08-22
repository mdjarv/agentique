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

/**
 * The slug as a human reads it — the suffix dropped. For display only:
 * routing, params, and lookups keep the qualified slug, which is the one
 * that's actually unique. Use this anywhere the machine is already named
 * beside it (`alltix-ui @zbook`), where the hash is noise repeating a fact
 * the row has already stated.
 */
export function displaySlug(slug: string): string {
  const cut = slug.lastIndexOf("~");
  return cut > 0 ? slug.slice(0, cut) : slug;
}
