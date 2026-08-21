/**
 * Feeds the wire — mounted once at the root so the feed accumulates
 * whether or not the landing page is open.
 *
 * The backend owns the durable coarse tier: `wire.list` backfills 48h of
 * persisted activity and `project.activity-item` streams live approvals,
 * results, and errors. The client adds what only it derives: schedule runs,
 * brain flares, commits — and the fine-resolution tier, where the ACTIVE
 * session streams at file granularity (focus = resolution).
 */
import { useEffect } from "react";
import { useWebSocket } from "~/hooks/useWebSocket";
import type { ActivityItem } from "~/lib/generated-types";
import { useBrainStore } from "~/stores/brain-store";
import { useChatStore } from "~/stores/chat-store";
import { usePulseStore } from "~/stores/pulse-store";
import { useScheduleStore } from "~/stores/schedule-store";
import { useWireStore, type WireEntry } from "./wire-store";

function sessionName(sessionId: string): string {
  const data = useChatStore.getState().sessions[sessionId];
  return data?.meta.name || "Untitled";
}

/**
 * Curated mapping from the backend feed. Tiering happens here: tool_use
 * items only survive for the active session; everything else is coarse
 * signal worth keeping for any session. Channel messages are not wire
 * material. Stable ids let backfill and live pushes converge.
 */
function mapActivityItem(item: ActivityItem, activeSessionId: string | null): WireEntry | null {
  if (item.kind !== "event") return null;
  const at = Date.parse(item.createdAt) || Date.now();
  const base = {
    id: `act-${item.itemId}`,
    at,
    sessionId: item.sourceId,
    strong: item.sourceName || "Untitled",
  };
  switch (item.eventType) {
    case "approval":
      return {
        ...base,
        kind: "attn",
        rest: "is waiting on approval",
        mono: item.content || undefined,
      };
    case "error":
      return { ...base, kind: "fail", rest: "errored", mono: item.content || undefined };
    case "result":
      return { ...base, kind: "state", rest: "finished a turn" };
    case "tool_use":
      if (item.sourceId !== activeSessionId) return null;
      return {
        ...base,
        kind: "tool",
        rest: "ran",
        mono: item.filePath || item.content || undefined,
      };
    default:
      return null;
  }
}

interface SessionSnapshot {
  state: string;
  approval: boolean;
  question: boolean;
  unseen: boolean;
}

export function useWireCapture(): void {
  const ws = useWebSocket();

  useEffect(() => {
    const add = useWireStore.getState().add;

    // Backfill the coarse tier from the backend, then keep it live below.
    ws.request("wire.list", { hours: 48, limit: 200 })
      .then((result) => {
        const items = result as ActivityItem[];
        const activeId = useChatStore.getState().activeSessionId;
        const mapped = items
          .map((item) => mapActivityItem(item, activeId))
          .filter((e): e is WireEntry => e !== null);
        useWireStore.getState().backfill(mapped);
      })
      .catch(() => {
        // Older servers without wire.list: the feed degrades to live-only.
      });

    const unsubActivity = ws.subscribe("project.activity-item", (item: ActivityItem) => {
      const entry = mapActivityItem(item, useChatStore.getState().activeSessionId);
      if (entry) useWireStore.getState().add(entry);
    });

    // ── chat store: state / approval / question / unseen transitions ──
    const snapshots = new Map<string, SessionSnapshot>();
    for (const [id, data] of Object.entries(useChatStore.getState().sessions)) {
      snapshots.set(id, {
        state: data.meta.state,
        approval: !!data.pendingApproval,
        question: !!data.pendingQuestion,
        unseen: data.hasUnseenCompletion,
      });
    }
    const unsubChat = useChatStore.subscribe((s) => {
      for (const [id, data] of Object.entries(s.sessions)) {
        const prev = snapshots.get(id);
        const next: SessionSnapshot = {
          state: data.meta.state,
          approval: !!data.pendingApproval,
          question: !!data.pendingQuestion,
          unseen: data.hasUnseenCompletion,
        };
        snapshots.set(id, next);
        if (!prev) continue;

        if (!prev.question && next.question) {
          add({
            at: Date.now(),
            kind: "attn",
            sessionId: id,
            strong: sessionName(id),
            rest: "asked a question",
          });
        }
        if (!prev.unseen && next.unseen) {
          add({
            at: Date.now(),
            kind: "state",
            sessionId: id,
            strong: sessionName(id),
            rest: "finished · unreviewed",
          });
        }
      }
    });

    // ── pulse store: commits, turn starts, and the fine-resolution tier ──
    // The active (selected) session streams at file resolution — every
    // touched file becomes an entry; every other session stays coarse
    // (turn starts only). Focus = resolution.
    const commitCounts = new Map<string, number>();
    const seenTurns = new Map<string, number>();
    const seenFiles = new Map<string, string>();
    const unsubPulse = usePulseStore.subscribe((s) => {
      const activeId = useChatStore.getState().activeSessionId;
      for (const [id, pulse] of Object.entries(s.pulses)) {
        const prevCommits = commitCounts.get(id) ?? 0;
        if (pulse.commitCount > prevCommits) {
          add({
            at: Date.now(),
            kind: "commit",
            sessionId: id,
            strong: sessionName(id),
            rest: `committed · ${pulse.commitCount} this turn`,
            mono: pulse.lastFilePath || undefined,
          });
        }
        commitCounts.set(id, pulse.commitCount);

        const prevTurn = seenTurns.get(id);
        if (pulse.turnStartedAt && pulse.turnStartedAt !== prevTurn && pulse.lastFilePath) {
          if (prevTurn !== undefined) {
            add({
              at: Date.now(),
              kind: "tool",
              sessionId: id,
              strong: sessionName(id),
              rest: "started a turn",
              mono: pulse.lastFilePath,
            });
          }
          seenTurns.set(id, pulse.turnStartedAt);
        }

        if (id === activeId && pulse.lastFilePath && pulse.lastFilePath !== seenFiles.get(id)) {
          seenFiles.set(id, pulse.lastFilePath);
          add({
            at: Date.now(),
            kind: "tool",
            sessionId: id,
            strong: sessionName(id),
            rest: "touched",
            mono: pulse.lastFilePath,
          });
        }
      }
    });

    // ── schedule store: fired runs ──
    const seenRuns = new Set<string>();
    for (const runs of Object.values(useScheduleStore.getState().runs)) {
      for (const run of runs) seenRuns.add(run.id);
    }
    const unsubSched = useScheduleStore.subscribe((s) => {
      for (const [scheduleId, runs] of Object.entries(s.runs)) {
        for (const run of runs) {
          if (seenRuns.has(run.id) || !run.firedAt) continue;
          seenRuns.add(run.id);
          const name = s.schedules[scheduleId]?.name || "schedule";
          add({
            at: Date.parse(run.firedAt) || Date.now(),
            kind: "sched",
            strong: name,
            rest: run.summary ? `fired · ${run.summary}` : "fired",
          });
        }
      }
    });

    // ── brain: the flare says something was learned ──
    let flareSeq = useBrainStore.getState().flareSeq;
    const unsubBrain = useBrainStore.subscribe((s) => {
      if (s.flareSeq === flareSeq) return;
      flareSeq = s.flareSeq;
      add({
        at: Date.now(),
        kind: "brain",
        strong: "brain",
        rest: "learned something new",
      });
    });

    return () => {
      unsubActivity();
      unsubChat();
      unsubPulse();
      unsubSched();
      unsubBrain();
    };
  }, [ws]);
}
