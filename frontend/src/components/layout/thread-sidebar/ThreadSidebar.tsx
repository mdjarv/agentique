/**
 * Flat session-first sidebar — orchestrator.
 *
 * Pinned (drag-orderable) / Open (attention-first) / Archived (collapsed).
 * Rows come from `useThreadGroups`; pin, archive, and reorder go over WS and
 * settle via the `session.pinned` / `session.state` pushes. Archive reuses
 * the existing mark-done primitive, so archiving the active session also
 * triggers the existing navigate-to-sibling behavior.
 */
import { DndContext } from "@dnd-kit/core";
import { SortableContext } from "@dnd-kit/sortable";
import { useNavigate } from "@tanstack/react-router";
import { useCallback, useState } from "react";
import { toast } from "sonner";
import { useWebSocket } from "~/hooks/useWebSocket";
import { markSessionDone, setSessionPinned, unmarkSessionDone } from "~/lib/session/actions";
import { cn, getErrorMessage, sessionShortId } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";
import { useChatStore } from "~/stores/chat-store";
import { StreamSearchBar } from "../StreamSearchBar";
import { ThreadRow } from "./ThreadRow";
import { CollapsibleBlock, ThreadSection } from "./ThreadSection";
import type { ThreadRowVM } from "./types";
import { usePinDnd, usePinSortable } from "./use-pin-dnd";
import { useThreadGroups } from "./use-thread-groups";

export function ThreadSidebar() {
  const navigate = useNavigate();
  const ws = useWebSocket();
  const [searchQuery, setSearchQuery] = useState("");
  const [archivedExpanded, setArchivedExpanded] = useState(false);
  const [staleExpanded, setStaleExpanded] = useState(false);
  const groups = useThreadGroups(searchQuery);
  const activeSessionId = useChatStore((s) => s.activeSessionId);

  const isSearching = searchQuery.trim().length > 0;
  const showArchived = isSearching || archivedExpanded;
  const isEmpty =
    groups.pinned.length === 0 &&
    groups.open.length === 0 &&
    groups.stale.length === 0 &&
    groups.archived.length === 0;

  const openSession = useCallback(
    (vm: ThreadRowVM) => {
      useAppStore.getState().setSidebarOpen(false);
      navigate({
        to: "/project/$projectSlug/session/$sessionShortId",
        params: { projectSlug: vm.projectSlug, sessionShortId: sessionShortId(vm.sessionId) },
      });
    },
    [navigate],
  );

  const togglePin = useCallback(
    (vm: ThreadRowVM) => {
      // New pins land at the end of the pinned section.
      const nextOrder = vm.pinned ? 0 : groups.pinned.length;
      setSessionPinned(ws, vm.sessionId, !vm.pinned, nextOrder).catch((err) =>
        toast.error(getErrorMessage(err, "Failed to update pin")),
      );
    },
    [ws, groups.pinned.length],
  );

  const archive = useCallback(
    (vm: ThreadRowVM) => {
      markSessionDone(ws, vm.sessionId).catch((err) =>
        toast.error(getErrorMessage(err, "Failed to archive session")),
      );
    },
    [ws],
  );

  const unarchive = useCallback(
    (vm: ThreadRowVM) => {
      unmarkSessionDone(ws, vm.sessionId).catch((err) =>
        toast.error(getErrorMessage(err, "Failed to unarchive session")),
      );
    },
    [ws],
  );

  // The shelf sweep: archive every stale session at once, with one undo.
  // Both directions settle per-session — a remote machine that is offline
  // (or runs a binary without the command) must not sink the whole sweep.
  const archiveAllStale = useCallback(() => {
    const ids = groups.stale.map((vm) => vm.sessionId);
    if (ids.length === 0) return;
    Promise.allSettled(ids.map((id) => markSessionDone(ws, id))).then((results) => {
      const archived = ids.filter((_, i) => results[i]?.status === "fulfilled");
      const failed = ids.length - archived.length;
      if (archived.length === 0) {
        toast.error("Failed to archive sessions");
        return;
      }
      toast(
        `Archived ${archived.length} session${archived.length !== 1 ? "s" : ""}` +
          (failed > 0 ? ` · ${failed} failed` : ""),
        {
          action: {
            label: "Undo",
            onClick: () => {
              Promise.allSettled(archived.map((id) => unmarkSessionDone(ws, id))).then((rs) => {
                const stuck = rs.filter((r) => r.status === "rejected").length;
                if (stuck > 0) {
                  toast.error(`${stuck} session${stuck !== 1 ? "s" : ""} could not be restored`);
                }
              });
            },
          },
        },
      );
    });
  }, [ws, groups.stale]);

  const pinnedIds = groups.pinned.map((vm) => vm.sessionId);
  const reorderPins = useCallback(
    (ids: string[]) => {
      Promise.all(ids.map((id, i) => setSessionPinned(ws, id, true, i))).catch((err) =>
        toast.error(getErrorMessage(err, "Failed to reorder pins")),
      );
    },
    [ws],
  );
  const dnd = usePinDnd(pinnedIds, reorderPins);

  return (
    <div className="flex min-h-0 min-w-0 flex-1 flex-col">
      <StreamSearchBar
        value={searchQuery}
        onChange={setSearchQuery}
        placeholder="Search sessions..."
      />

      <div className="min-h-0 flex-1 overflow-y-auto px-2 py-2">
        {groups.pinned.length > 0 && (
          <ThreadSection label="Pinned" count={groups.pinned.length}>
            <DndContext sensors={dnd.sensors} onDragEnd={dnd.handleDragEnd}>
              <SortableContext items={pinnedIds} strategy={dnd.strategy}>
                {groups.pinned.map((vm) => (
                  <SortablePinnedRow
                    key={vm.sessionId}
                    vm={vm}
                    selected={vm.sessionId === activeSessionId}
                    onClick={() => openSession(vm)}
                    onTogglePin={() => togglePin(vm)}
                    onArchive={() => archive(vm)}
                  />
                ))}
              </SortableContext>
            </DndContext>
          </ThreadSection>
        )}

        <ThreadSection
          label="Open"
          count={groups.open.length}
          className={cn(groups.pinned.length > 0 && "mt-2")}
        >
          {groups.open.map((vm) => (
            <ThreadRow
              key={vm.sessionId}
              vm={vm}
              selected={vm.sessionId === activeSessionId}
              onClick={() => openSession(vm)}
              onTogglePin={() => togglePin(vm)}
              onArchive={() => archive(vm)}
            />
          ))}
          {groups.open.length === 0 && !isEmpty && (
            <div className="px-2.5 py-2 text-xs text-muted-foreground-faint">
              Nothing open — start one with New.
            </div>
          )}
        </ThreadSection>

        {groups.stale.length > 0 && (
          <CollapsibleBlock
            label="Finished earlier"
            count={groups.stale.length}
            expanded={staleExpanded}
            onToggle={() => setStaleExpanded((e) => !e)}
            className="mt-2"
            action={
              <button
                type="button"
                onClick={archiveAllStale}
                className="shrink-0 cursor-pointer rounded-full px-2 py-0.5 text-[10px] font-semibold text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground-bright"
              >
                Archive all
              </button>
            }
          >
            {groups.stale.map((vm) => (
              <ThreadRow
                key={vm.sessionId}
                vm={vm}
                selected={vm.sessionId === activeSessionId}
                onClick={() => openSession(vm)}
                onTogglePin={() => togglePin(vm)}
                onArchive={() => archive(vm)}
              />
            ))}
          </CollapsibleBlock>
        )}

        {(groups.archived.length > 0 || archivedExpanded) && (
          <CollapsibleBlock
            label="Archived"
            count={groups.archived.length}
            expanded={showArchived}
            onToggle={() => setArchivedExpanded((e) => !e)}
            className="mt-2"
          >
            {groups.archived.map((vm) => (
              <ThreadRow
                key={vm.sessionId}
                vm={vm}
                selected={vm.sessionId === activeSessionId}
                onClick={() => openSession(vm)}
                onTogglePin={() => togglePin(vm)}
                onArchive={() => unarchive(vm)}
              />
            ))}
          </CollapsibleBlock>
        )}

        {isEmpty && (
          <div className="px-3 py-8 text-center text-xs text-muted-foreground-faint">
            {isSearching ? (
              <>No sessions match “{searchQuery.trim()}”</>
            ) : (
              <>No sessions yet — pick a project with New.</>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

/** A pinned ThreadRow wrapped with the dnd-kit sortable handle. */
function SortablePinnedRow({
  vm,
  selected,
  onClick,
  onTogglePin,
  onArchive,
}: {
  vm: ThreadRowVM;
  selected: boolean;
  onClick: () => void;
  onTogglePin: () => void;
  onArchive: () => void;
}) {
  const sortable = usePinSortable(vm.sessionId);
  return (
    <div
      ref={sortable.setNodeRef}
      style={sortable.style}
      {...sortable.attributes}
      {...sortable.listeners}
    >
      <ThreadRow
        vm={vm}
        selected={selected}
        onClick={onClick}
        onTogglePin={onTogglePin}
        onArchive={onArchive}
      />
    </div>
  );
}
