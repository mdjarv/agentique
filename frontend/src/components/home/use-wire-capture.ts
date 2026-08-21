/**
 * Feeds the wire from store transitions — mounted once at the root so the
 * feed accumulates whether or not the landing page is open. Pure observer:
 * it subscribes to the chat / pulse / schedule / brain stores and emits
 * entries on meaningful deltas; it never mutates them.
 */
import { useEffect } from "react";
import { useBrainStore } from "~/stores/brain-store";
import { type SessionData, useChatStore } from "~/stores/chat-store";
import { usePulseStore } from "~/stores/pulse-store";
import { useScheduleStore } from "~/stores/schedule-store";
import { useWireStore, type WireEntry } from "./wire-store";

function sessionName(sessionId: string): string {
  const data = useChatStore.getState().sessions[sessionId];
  return data?.meta.name || "Untitled";
}

function approvalMono(sessionId: string): string | undefined {
  const data = useChatStore.getState().sessions[sessionId];
  const approval = data?.pendingApproval;
  if (!approval) return undefined;
  const input = approval.input as Record<string, unknown> | null;
  const command = input && typeof input.command === "string" ? input.command : "";
  return command ? `${approval.toolName} · ${command}` : approval.toolName;
}

interface SessionSnapshot {
  state: string;
  approval: boolean;
  question: boolean;
  unseen: boolean;
}

export function useWireCapture(): void {
  useEffect(() => {
    const add = useWireStore.getState().add;

    // First load: seed from recent completions so the river isn't empty.
    // Lazy — session.list lands per project after mount, so keep trying on
    // every store tick until a seed actually takes (the store being
    // non-empty is what stops it).
    const trySeed = (sessions: Record<string, SessionData>) => {
      if (useWireStore.getState().entries.length > 0) return;
      if (Object.keys(sessions).length === 0) return;
      const now = Date.now();
      const seedEntries: Omit<WireEntry, "id">[] = [];
      for (const data of Object.values(sessions)) {
        const completedAt = data.meta.completedAt ? Date.parse(data.meta.completedAt) : 0;
        if (completedAt && now - completedAt < 48 * 3600_000) {
          seedEntries.push({
            at: completedAt,
            kind: "state",
            sessionId: data.meta.id,
            strong: data.meta.name || "Untitled",
            rest: "was archived",
          });
        }
      }
      useWireStore.getState().seed(seedEntries);
    };
    trySeed(useChatStore.getState().sessions);

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
      trySeed(s.sessions);
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

        if (!prev.approval && next.approval) {
          add({
            at: Date.now(),
            kind: "attn",
            sessionId: id,
            strong: sessionName(id),
            rest: "is waiting on approval",
            mono: approvalMono(id),
          });
        }
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
        if (prev.state !== "failed" && next.state === "failed") {
          add({
            at: Date.now(),
            kind: "attn",
            sessionId: id,
            strong: sessionName(id),
            rest: "failed",
          });
        }
      }
    });

    // ── pulse store: commits and turn starts ──
    const commitCounts = new Map<string, number>();
    const seenTurns = new Map<string, number>();
    const unsubPulse = usePulseStore.subscribe((s) => {
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
      unsubChat();
      unsubPulse();
      unsubSched();
      unsubBrain();
    };
  }, []);
}
