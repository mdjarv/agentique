import type { SessionMetadata } from "~/stores/chat-types";

export interface HierarchyTreeNode {
  session: SessionMetadata;
  children: HierarchyTreeNode[];
}

/**
 * Groups sessions into a parent→children tree. Sessions whose referenced
 * parent is not in the current map (e.g. dangling pointers, or the parent
 * lives in a project that's paged out) are treated as roots.
 *
 * Only roots that actually have children are returned, so the caller can
 * keep the "Session hierarchy" section empty when every session is solo.
 */
export function buildSessionHierarchy(
  sessions: Record<string, { meta: SessionMetadata }>,
): HierarchyTreeNode[] {
  const metas: SessionMetadata[] = Object.values(sessions).map((s) => s.meta);
  const byId = new Map<string, SessionMetadata>();
  for (const m of metas) byId.set(m.id, m);

  const childrenOf = new Map<string, SessionMetadata[]>();
  for (const m of metas) {
    const parent = m.parentSessionId;
    if (!parent || !byId.has(parent)) continue;
    const list = childrenOf.get(parent);
    if (list) list.push(m);
    else childrenOf.set(parent, [m]);
  }

  function buildNode(m: SessionMetadata): HierarchyTreeNode {
    const kids = childrenOf.get(m.id) ?? [];
    return {
      session: m,
      children: kids.map(buildNode),
    };
  }

  const roots: HierarchyTreeNode[] = [];
  for (const m of metas) {
    const hasKnownParent = m.parentSessionId && byId.has(m.parentSessionId);
    if (hasKnownParent) continue;
    const node = buildNode(m);
    if (node.children.length > 0) roots.push(node);
  }
  roots.sort((a, b) => a.session.name.localeCompare(b.session.name));
  return roots;
}
