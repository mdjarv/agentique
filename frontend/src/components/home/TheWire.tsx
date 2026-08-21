/**
 * The wire — the landing page's ambient event river. Renders the locally
 * captured feed (wire-store) newest-first with kind filters. Clicking an
 * entry with a session opens it.
 */
import { useNavigate } from "@tanstack/react-router";
import { useMemo, useState } from "react";
import { cn, relativeTime, sessionShortId } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";
import { useChatStore } from "~/stores/chat-store";
import { useWireStore, type WireEntry, type WireKind } from "./wire-store";

const DOT_CLASS: Record<WireKind, string> = {
  commit: "bg-success",
  tool: "bg-teal",
  sched: "bg-info",
  brain: "bg-agent",
  state: "bg-muted-foreground-faint",
  attn: "bg-orange animate-pulse motion-reduce:animate-none",
};

const FILTERS: { key: WireKind | "all"; label: string }[] = [
  { key: "all", label: "All" },
  { key: "commit", label: "Commits" },
  { key: "tool", label: "Turns" },
  { key: "sched", label: "Schedules" },
  { key: "brain", label: "Brain" },
];

function timeLabel(at: number): string {
  const d = new Date(at);
  const today = new Date().toDateString() === d.toDateString();
  if (today) {
    return `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
  }
  return relativeTime(d.toISOString());
}

export function TheWire() {
  const entries = useWireStore((s) => s.entries);
  const [filter, setFilter] = useState<WireKind | "all">("all");
  const navigate = useNavigate();
  const projects = useAppStore((s) => s.projects);

  const visible = useMemo(
    () => (filter === "all" ? entries : entries.filter((e) => e.kind === filter)),
    [entries, filter],
  );

  const openEntry = (e: WireEntry) => {
    if (!e.sessionId) return;
    const data = useChatStore.getState().sessions[e.sessionId];
    if (!data) return;
    const slug = projects.find((p) => p.id === data.meta.projectId)?.slug;
    if (!slug) return;
    navigate({
      to: "/project/$projectSlug/session/$sessionShortId",
      params: { projectSlug: slug, sessionShortId: sessionShortId(e.sessionId) },
    });
  };

  return (
    <div>
      <div className="mb-3 flex items-center gap-1.5">
        <span className="mr-2 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
          The wire
        </span>
        {FILTERS.map((f) => (
          <button
            key={f.key}
            type="button"
            onClick={() => setFilter(f.key)}
            className={cn(
              "cursor-pointer rounded-full border border-transparent px-2.5 py-1 text-[10.5px] font-semibold",
              filter === f.key
                ? "border-primary/40 bg-primary/10 text-primary"
                : "text-muted-foreground-dim hover:bg-secondary hover:text-foreground",
            )}
          >
            {f.label}
          </button>
        ))}
      </div>

      {visible.length === 0 ? (
        <div className="py-6 text-[12.5px] text-muted-foreground-faint">
          The wire is quiet — events appear here as agents work.
        </div>
      ) : (
        <div className="border-l border-border/60">
          {visible.map((e) => (
            <button
              key={e.id}
              type="button"
              onClick={() => openEntry(e)}
              disabled={!e.sessionId}
              className={cn(
                "relative flex w-full items-baseline gap-2.5 py-1 pl-3.5 text-left text-[12.5px]",
                e.sessionId && "cursor-pointer hover:bg-sidebar-accent/40",
              )}
            >
              <span
                className={cn(
                  "absolute -left-[3.5px] top-[11px] size-1.5 rounded-full",
                  DOT_CLASS[e.kind],
                )}
              />
              <span className="w-11 shrink-0 font-mono text-[10px] tabular-nums text-muted-foreground-faint">
                {timeLabel(e.at)}
              </span>
              <span className="min-w-0 text-muted-foreground-dim">
                <span className="font-semibold text-foreground-bright">{e.strong}</span> {e.rest}
                {e.mono && (
                  <span className="font-mono text-[11px] text-muted-foreground"> {e.mono}</span>
                )}
              </span>
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
