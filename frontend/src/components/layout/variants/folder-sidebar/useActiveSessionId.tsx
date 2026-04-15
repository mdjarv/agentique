import type { ReactNode } from "react";
import { useChatStore } from "~/stores/chat-store";
import { SidebarRow } from "./SidebarRow";
import type { TodoProgress } from "./types";

/** Returns true if the given session ID is the currently active session.
 *  Returns a primitive boolean — only the two affected rows re-render
 *  when the active session changes (old → false, new → true). */
export function useIsActiveSession(sessionId: string): boolean {
  return useChatStore((s) => s.activeSessionId === sessionId);
}

/** SidebarRow that determines its own selection state from the store.
 *  Isolates selection re-renders to individual rows instead of cascading
 *  through the entire sidebar tree. */
export function SessionSidebarRow({
  sessionId,
  indent,
  compact,
  onClick,
  todoProgress,
  children,
}: {
  sessionId: string;
  indent: number;
  compact?: boolean;
  onClick: () => void;
  todoProgress?: TodoProgress;
  children: ReactNode;
}) {
  const selected = useIsActiveSession(sessionId);
  return (
    <SidebarRow
      indent={indent}
      selected={selected}
      compact={compact}
      onClick={onClick}
      todoProgress={todoProgress}
    >
      {children}
    </SidebarRow>
  );
}
