/**
 * How a project is named in a row.
 *
 * The label is the project's **name**, never its slug. A slug is an identity:
 * it routes, it is unique per server, and it is derived from the name once, at
 * creation. So it lags a rename, and it can only spell `[a-z0-9-]` — a project
 * named "Träffbild" is registered as `traffbild`, and one named "The Pint" as
 * `the-pint`. A row that showed the slug reported the wrong thing twice: the
 * name a project used to have, and an ASCII flattening of it always.
 *
 * Presentation comes from the representative project (docs/multi-machine.md),
 * whose name carries no machine suffix — so unlike a slug, nothing has to be
 * stripped before it is shown.
 *
 * One module, because four surfaces render this pair (the sidebar's session
 * rows, its draft rows, the landing deck and SyncDock) and a project that reads
 * "Träffbild" in the rail cannot read "traffbild" on the overview.
 */

/** The name as a row shows it, falling back to the slug for a nameless row. */
export function projectLabel(name: string, slug: string): string {
  return name.trim() || slug;
}

/**
 * Up to two letters for the row's avatar. Two words give their initials
 * ("The Pint" -> TP, "meta-spec" -> MS); one word gives its first two letters,
 * because a lone "T" identifies nothing in a list of projects.
 */
export function projectInitials(label: string): string {
  const [first, second] = label.split(/[\s\-_/&.]+/).filter(Boolean);
  if (!first) return "?";
  if (!second) return first.slice(0, 2).toUpperCase();
  return (first.slice(0, 1) + second.slice(0, 1)).toUpperCase();
}
