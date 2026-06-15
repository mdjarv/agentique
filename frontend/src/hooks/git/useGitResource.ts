import { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { useWebSocket } from "~/hooks/useWebSocket";
import { getErrorMessage } from "~/lib/utils";
import type { WsClient } from "~/lib/ws-client";
import { useChatStore } from "~/stores/chat-store";

/** A lazily-fetched, git-versioned session resource (diff, uncommitted diff, commit log). */
export interface GitResource<T> {
  data: T | null;
  loading: boolean;
  /** Fetch now (or clear when `enabled` is false). Resolves to the data, or null on miss/error. */
  refetch: () => Promise<T | null>;
}

export interface UseGitResourceOptions<T> {
  sessionId: string;
  /** Performs the fetch. Only invoked when `enabled` is true; must be a stable reference. */
  fetch: (ws: WsClient, sessionId: string) => Promise<T>;
  /** Whether the resource is eligible to load for the session's current git state. */
  enabled: boolean;
  /** Re-fetch when the session transitions from running → idle. */
  fetchOnIdle?: boolean;
  /** Toast shown on fetch failure; when omitted, failures clear the data silently. */
  errorMessage?: string;
}

/** State carries its owning sessionId so a stale fetch from a previous session reads as empty. */
interface GitResourceState<T> {
  sessionId: string;
  data: T | null;
  loading: boolean;
}

/**
 * Shared state machine for git-versioned session resources. Replaces the
 * hand-rolled ref juggling (in-render setState reset, overlapping fetch effects)
 * that was duplicated across useSessionDiff / useUncommittedDiff / useCommitLog.
 *
 * Fetches eagerly for an idle, eligible session, and re-fetches on running→idle
 * transitions (when `fetchOnIdle`) and on every gitVersion bump. A monotonic
 * fetch token drops superseded responses; state is scoped to its sessionId so a
 * late response from a previous session can never leak into the current one.
 */
export function useGitResource<T>({
  sessionId,
  fetch,
  enabled,
  fetchOnIdle = false,
  errorMessage,
}: UseGitResourceOptions<T>): GitResource<T> {
  const ws = useWebSocket();
  const meta = useChatStore((s) => s.sessions[sessionId]?.meta);
  const isRunning = meta?.state === "running";
  const gitVersion = meta?.gitVersion ?? 0;

  const [state, setState] = useState<GitResourceState<T>>(() => ({
    sessionId,
    data: null,
    loading: false,
  }));

  // Session-scoped refs that gate the effects below. Reset together — and only
  // refs, never state — when the session changes, so there is no render-phase
  // setState. Stale data from the previous session is masked by the read below.
  const wasRunning = useRef(isRunning);
  const prevVersion = useRef(gitVersion);
  const didInitialFetch = useRef(false);
  const prevSessionId = useRef(sessionId);
  if (prevSessionId.current !== sessionId) {
    prevSessionId.current = sessionId;
    wasRunning.current = isRunning;
    prevVersion.current = gitVersion;
    didInitialFetch.current = false;
  }

  const fetchToken = useRef(0);

  const refetch = useCallback(async (): Promise<T | null> => {
    if (!enabled) {
      fetchToken.current++;
      setState({ sessionId, data: null, loading: false });
      return null;
    }
    const token = ++fetchToken.current;
    setState((s) =>
      s.sessionId === sessionId
        ? { ...s, loading: true }
        : { sessionId, data: null, loading: true },
    );
    try {
      const result = await fetch(ws, sessionId);
      if (fetchToken.current !== token) return null;
      setState({ sessionId, data: result, loading: false });
      return result;
    } catch (err) {
      if (fetchToken.current !== token) return null;
      if (errorMessage) {
        toast.error(getErrorMessage(err, errorMessage));
        setState((s) => (s.sessionId === sessionId ? { ...s, loading: false } : s));
      } else {
        setState({ sessionId, data: null, loading: false });
      }
      return null;
    }
  }, [ws, sessionId, enabled, fetch, errorMessage]);

  // Eager fetch once for a non-running, eligible session.
  useEffect(() => {
    if (!didInitialFetch.current && enabled && !isRunning) {
      didInitialFetch.current = true;
      refetch();
    }
  }, [enabled, isRunning, refetch]);

  // Re-fetch (refetch clears when no longer enabled) on running → idle.
  useEffect(() => {
    if (fetchOnIdle && wasRunning.current && !isRunning) refetch();
    wasRunning.current = isRunning;
  }, [fetchOnIdle, isRunning, refetch]);

  // Re-fetch (or clear) on every gitVersion bump while idle — covers commit,
  // merge, rebase, clean.
  useEffect(() => {
    if (gitVersion !== prevVersion.current && !isRunning) {
      prevVersion.current = gitVersion;
      refetch();
    }
  }, [gitVersion, isRunning, refetch]);

  const current = state.sessionId === sessionId;
  return {
    data: current ? state.data : null,
    loading: current ? state.loading : false,
    refetch,
  };
}
