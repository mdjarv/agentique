import { Link } from "@tanstack/react-router";
import { ChevronDown, ChevronRight, Network, Scissors, Trash2 } from "lucide-react";
import { useMemo, useState } from "react";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "~/components/ui/alert-dialog";
import { useSessionTeardown } from "~/hooks/session/useSessionTeardown";
import { countDescendants, type HierarchyTreeNode } from "~/lib/session-hierarchy";
import { cn } from "~/lib/utils";
import type { SessionMetadata } from "~/stores/chat-types";

interface ProjectRef {
  id: string;
  name: string;
  slug: string;
}

/** Renders the parent→children session tree with per-node delete/dissolve. */
export function SessionHierarchy({
  nodes,
  projects,
}: {
  nodes: HierarchyTreeNode[];
  projects: ProjectRef[];
}) {
  return (
    <div className="space-y-1">
      {nodes.map((node) => (
        <HierarchyNode key={node.session.id} node={node} projects={projects} />
      ))}
    </div>
  );
}

function HierarchyNode({
  node,
  projects,
  depth = 0,
}: {
  node: HierarchyTreeNode;
  projects: ProjectRef[];
  depth?: number;
}) {
  const [expanded, setExpanded] = useState(true);
  const teardown = useSessionTeardown(node.session.id);
  const hasChildren = node.children.length > 0;
  const project = projects.find((p) => p.id === node.session.projectId);
  const projectName = project?.name ?? "";
  const projectSlug = project?.slug ?? "";
  const stateColor = stateDotColor(node.session.state);
  const shortId = node.session.id.split("-")[0] ?? node.session.id;
  const descendantCount = countDescendants(node);

  // Channels this session is a lead of — used to expose the Dissolve action
  // only where it has a defined effect.
  const leadChannelIds = useMemo(() => {
    const roles = node.session.channelRoles ?? {};
    return Object.entries(roles)
      .filter(([, role]) => role === "lead")
      .map(([id]) => id);
  }, [node.session.channelRoles]);
  const canDissolve = leadChannelIds.length > 0;

  return (
    <div>
      <div
        className="group flex items-center gap-2 rounded-md px-2 py-1.5 hover:bg-accent/40 transition-colors"
        style={{ paddingLeft: `${8 + depth * 16}px` }}
      >
        <button
          type="button"
          aria-label={expanded ? "Collapse" : "Expand"}
          onClick={() => hasChildren && setExpanded((v) => !v)}
          className={cn(
            "flex h-4 w-4 shrink-0 items-center justify-center text-muted-foreground",
            !hasChildren && "invisible",
          )}
        >
          {hasChildren &&
            (expanded ? <ChevronDown className="size-3" /> : <ChevronRight className="size-3" />)}
        </button>
        <span
          className={cn("size-2 shrink-0 rounded-full", stateColor)}
          title={`State: ${node.session.state}`}
        />
        <Network className="size-3 shrink-0 text-muted-foreground" />
        {projectSlug ? (
          <Link
            to="/project/$projectSlug/session/$sessionShortId"
            params={{ projectSlug, sessionShortId: shortId }}
            className="flex min-w-0 flex-1 items-center gap-2 truncate hover:underline"
          >
            <span className="truncate text-sm font-medium">{node.session.name}</span>
            {hasChildren && (
              <span className="text-[10px] text-muted-foreground tabular-nums">
                {node.children.length}
              </span>
            )}
          </Link>
        ) : (
          <span className="flex min-w-0 flex-1 items-center gap-2 truncate">
            <span className="truncate text-sm font-medium">{node.session.name}</span>
            {hasChildren && (
              <span className="text-[10px] text-muted-foreground tabular-nums">
                {node.children.length}
              </span>
            )}
          </span>
        )}
        {projectName && (
          <span className="truncate text-[10px] text-muted-foreground">{projectName}</span>
        )}
        {canDissolve && (
          <button
            type="button"
            aria-label={`Dissolve channel led by ${node.session.name}`}
            title="Dissolve — stop workers, keep this session"
            onClick={(e) => {
              e.stopPropagation();
              teardown.setDissolveOpen(true);
            }}
            className="shrink-0 rounded p-1 text-muted-foreground opacity-0 transition-opacity hover:bg-amber-500/10 hover:text-amber-600 group-hover:opacity-100 focus:opacity-100"
          >
            <Scissors className="size-3" />
          </button>
        )}
        <button
          type="button"
          aria-label={`Delete ${node.session.name}`}
          title={
            descendantCount > 0
              ? `Delete — remove this session and ${descendantCount} descendant(s)`
              : "Delete — remove this session"
          }
          onClick={(e) => {
            e.stopPropagation();
            teardown.setConfirmOpen(true);
          }}
          className="shrink-0 rounded p-1 text-muted-foreground opacity-0 transition-opacity hover:bg-destructive/10 hover:text-destructive group-hover:opacity-100 focus:opacity-100"
        >
          <Trash2 className="size-3" />
        </button>
      </div>
      {hasChildren && expanded && (
        <div>
          {node.children.map((c) => (
            <HierarchyNode key={c.session.id} node={c} projects={projects} depth={depth + 1} />
          ))}
        </div>
      )}
      <AlertDialog open={teardown.confirmOpen} onOpenChange={teardown.setConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete {node.session.name}?</AlertDialogTitle>
            <AlertDialogDescription>
              {descendantCount > 0 ? (
                <>
                  This session has <strong>{descendantCount}</strong> descendant session
                  {descendantCount === 1 ? "" : "s"} that will be deleted with it. Each descendant's
                  worktree and branch will be removed. This cannot be undone.
                </>
              ) : (
                <>
                  This will stop the session, remove its worktree and branch, and clear its history.
                  This cannot be undone.
                </>
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={teardown.deleting}>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => teardown.runDelete({ name: node.session.name, descendantCount })}
              disabled={teardown.deleting}
            >
              {teardown.deleting ? "Deleting…" : "Delete"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      <AlertDialog open={teardown.dissolveOpen} onOpenChange={teardown.setDissolveOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Dissolve {node.session.name}'s channel?</AlertDialogTitle>
            <AlertDialogDescription>
              Stops every worker in{" "}
              {leadChannelIds.length === 1
                ? "the channel"
                : `${leadChannelIds.length} channels led by this session`}
              , removes their worktrees and branches, and deletes the channel.{" "}
              <strong>{node.session.name} itself stays alive</strong> as a regular session — its
              worktree and history are preserved. This cannot be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={teardown.dissolving}>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={() =>
                teardown.runDissolve({ name: node.session.name, channelIds: leadChannelIds })
              }
              disabled={teardown.dissolving}
            >
              {teardown.dissolving ? "Dissolving…" : "Dissolve"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function stateDotColor(state: SessionMetadata["state"]): string {
  switch (state) {
    case "running":
      return "bg-emerald-500";
    case "idle":
      return "bg-sky-500";
    case "merging":
      return "bg-amber-500";
    case "failed":
      return "bg-red-500";
    case "done":
    case "stopped":
      return "bg-muted-foreground/40";
    default:
      return "bg-muted-foreground/40";
  }
}
