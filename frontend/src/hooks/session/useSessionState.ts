import { useShallow } from "zustand/shallow";
import type {
  AutoApproveMode,
  ContextUsage,
  PendingApproval,
  PendingQuestion,
  SessionMetadata,
  TodoItem,
  Turn,
} from "~/stores/chat-store";
import { useChatStore } from "~/stores/chat-store";

const EMPTY_TURNS: Turn[] = [];

interface SessionState {
  turns: Turn[];
  meta: SessionMetadata | undefined;
  pendingApproval: PendingApproval | null;
  pendingQuestion: PendingQuestion | null;
  planMode: boolean;
  autoApproveMode: AutoApproveMode;
  todos: TodoItem[] | null;
  contextUsage: ContextUsage | null;
  compacting: boolean;
}

export function useSessionState(sessionId: string): SessionState {
  const turns = useChatStore((s) => s.sessions[sessionId]?.turns ?? EMPTY_TURNS);

  const rest = useChatStore(
    useShallow((s) => {
      const session = s.sessions[sessionId];
      return {
        meta: session?.meta,
        pendingApproval: session?.pendingApproval ?? null,
        pendingQuestion: session?.pendingQuestion ?? null,
        planMode: session?.planMode ?? false,
        autoApproveMode: session?.autoApproveMode ?? ("manual" as AutoApproveMode),
        todos: session?.todos ?? null,
        contextUsage: session?.contextUsage ?? null,
        compacting: session?.compacting ?? false,
      };
    }),
  );

  return { turns, ...rest };
}
