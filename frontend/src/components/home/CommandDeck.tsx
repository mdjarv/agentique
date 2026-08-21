/**
 * The landing page's triage bands — needs-you (with inline resolve), live
 * now, and unread-to-review. Every band renders only when non-empty; when
 * all three are empty the deck reduces to a single all-quiet line and the
 * wire below owns the page.
 */
import { useNavigate } from "@tanstack/react-router";
import { useMemo } from "react";
import { toast } from "sonner";
import { formatPulse } from "~/components/layout/session/PulseStatus";
import { useWebSocket } from "~/hooks/useWebSocket";
import { resolveApproval } from "~/lib/session/actions";
import { cn, getErrorMessage, relativeTime, sessionShortId } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";
import { type SessionData, useChatStore } from "~/stores/chat-store";
import { usePulseStore } from "~/stores/pulse-store";

function SectionLabel({ children }: { children: React.ReactNode }) {
  return (
    <div className="mb-2 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
      {children}
    </div>
  );
}

function useOpenSession() {
  const navigate = useNavigate();
  const projects = useAppStore((s) => s.projects);
  return (data: SessionData) => {
    const slug = projects.find((p) => p.id === data.meta.projectId)?.slug;
    if (!slug) return;
    navigate({
      to: "/project/$projectSlug/session/$sessionShortId",
      params: { projectSlug: slug, sessionShortId: sessionShortId(data.meta.id) },
    });
  };
}

function projectSlugOf(data: SessionData, projects: { id: string; slug: string }[]): string {
  return projects.find((p) => p.id === data.meta.projectId)?.slug ?? "";
}

function NeedsYouCard({ data }: { data: SessionData }) {
  const ws = useWebSocket();
  const open = useOpenSession();
  const projects = useAppStore((s) => s.projects);
  const approval = data.pendingApproval;
  const question = data.pendingQuestion;

  const summary = approval
    ? (() => {
        const input = approval.input as Record<string, unknown> | null;
        const command = input && typeof input.command === "string" ? input.command : "";
        return command ? `${approval.toolName} · ${command}` : approval.toolName;
      })()
    : question?.questions[0]
      ? `"${question.questions[0].question}"`
      : "";

  const resolve = (allow: boolean) => {
    if (!approval) return;
    resolveApproval(ws, data.meta.id, approval.approvalId, allow).catch((err) =>
      toast.error(getErrorMessage(err, "Failed to resolve approval")),
    );
  };

  return (
    <div className="max-w-md flex-1 rounded-xl border border-orange/40 bg-orange/5 p-3.5">
      <div className="mb-1 flex items-center gap-2">
        <span className="size-2 shrink-0 animate-pulse rounded-full bg-orange motion-reduce:animate-none" />
        <span className="min-w-0 flex-1 truncate text-[13.5px] font-semibold text-foreground-bright">
          {data.meta.name || "Untitled"}
        </span>
        <span className="shrink-0 font-mono text-[10px] text-muted-foreground">
          {projectSlugOf(data, projects)}
        </span>
      </div>
      <div className="mb-2 truncate font-mono text-[11px] text-orange">{summary}</div>
      <div className="flex gap-1.5">
        {approval ? (
          <>
            <button
              type="button"
              onClick={() => resolve(true)}
              className="cursor-pointer rounded-md bg-success px-3 py-1.5 text-[11px] font-semibold text-primary-foreground hover:opacity-90"
            >
              Allow
            </button>
            <button
              type="button"
              onClick={() => resolve(false)}
              className="cursor-pointer rounded-md px-2.5 py-1.5 text-[11px] font-semibold text-destructive hover:bg-destructive/10"
            >
              Deny
            </button>
          </>
        ) : (
          <button
            type="button"
            onClick={() => open(data)}
            className="cursor-pointer rounded-md px-2.5 py-1.5 text-[11px] font-semibold text-foreground hover:bg-secondary"
          >
            Answer…
          </button>
        )}
        <button
          type="button"
          onClick={() => open(data)}
          className="cursor-pointer rounded-md px-2.5 py-1.5 text-[11px] font-semibold text-muted-foreground hover:bg-secondary hover:text-foreground"
        >
          open ›
        </button>
      </div>
    </div>
  );
}

function LiveCard({ data }: { data: SessionData }) {
  const open = useOpenSession();
  const projects = useAppStore((s) => s.projects);
  const pulse = usePulseStore((s) => s.pulses[data.meta.id]);
  const todoTotal = data.todos?.length ?? 0;
  const todoDone = data.todos?.filter((t) => t.status === "completed").length ?? 0;

  return (
    <button
      type="button"
      onClick={() => open(data)}
      className="max-w-md flex-1 cursor-pointer rounded-xl border border-border/60 bg-card p-3 text-left transition-colors hover:border-border"
    >
      <div className="flex items-center gap-2">
        <span className="size-[7px] shrink-0 rounded-full bg-teal" />
        <span className="min-w-0 flex-1 truncate text-[13px] font-semibold text-foreground-bright">
          {data.meta.name || "Untitled"}
        </span>
        <span className="shrink-0 font-mono text-[10px] text-muted-foreground">
          {projectSlugOf(data, projects)}
        </span>
      </div>
      {pulse && (
        <div className="mt-1 truncate font-mono text-[10.5px] text-teal">{formatPulse(pulse)}</div>
      )}
      {todoTotal > 0 && (
        <div className="mt-2 h-[3px] overflow-hidden rounded-full bg-border/60">
          <div
            className="h-full rounded-full bg-primary"
            style={{ width: `${Math.round((todoDone / todoTotal) * 100)}%` }}
          />
        </div>
      )}
    </button>
  );
}

export function CommandDeck() {
  const sessions = useChatStore((s) => s.sessions);
  const open = useOpenSession();
  const projects = useAppStore((s) => s.projects);

  const { needs, live, review } = useMemo(() => {
    const all = Object.values(sessions).filter((d) => !d.meta.completedAt);
    return {
      needs: all.filter((d) => d.pendingApproval || d.pendingQuestion),
      live: all.filter(
        (d) => d.meta.state === "running" && !d.pendingApproval && !d.pendingQuestion,
      ),
      review: all.filter((d) => d.hasUnseenCompletion && d.meta.state !== "running"),
    };
  }, [sessions]);

  if (needs.length === 0 && live.length === 0 && review.length === 0) {
    return (
      <div className="flex items-center gap-2.5 text-[12.5px] text-muted-foreground">
        <span className="size-2 rounded-full bg-success opacity-70" />
        All quiet — nothing needs you.
      </div>
    );
  }

  return (
    <div className={cn("flex flex-col gap-4")}>
      {needs.length > 0 && (
        <div>
          <SectionLabel>
            Needs you <span className="font-mono text-muted-foreground-faint">{needs.length}</span>
          </SectionLabel>
          <div className="flex flex-wrap gap-3">
            {needs.map((d) => (
              <NeedsYouCard key={d.meta.id} data={d} />
            ))}
          </div>
        </div>
      )}

      {live.length > 0 && (
        <div>
          <SectionLabel>
            Live now <span className="font-mono text-muted-foreground-faint">{live.length}</span>
          </SectionLabel>
          <div className="flex flex-wrap gap-3">
            {live.map((d) => (
              <LiveCard key={d.meta.id} data={d} />
            ))}
          </div>
        </div>
      )}

      {review.length > 0 && (
        <div>
          <SectionLabel>
            To review <span className="font-mono text-muted-foreground-faint">{review.length}</span>
          </SectionLabel>
          <div className="flex flex-wrap gap-2">
            {review.map((d) => (
              <button
                key={d.meta.id}
                type="button"
                onClick={() => open(d)}
                className="flex cursor-pointer items-center gap-2 rounded-lg border border-border/60 bg-card px-3 py-2 text-[12.5px] transition-colors hover:border-border"
              >
                <span className="size-[7px] rounded-full bg-success" />
                <span className="font-semibold text-foreground-bright">
                  {d.meta.name || "Untitled"}
                </span>
                <span className="font-mono text-[10px] text-muted-foreground">
                  {projectSlugOf(d, projects)}
                </span>
                <span className="font-mono text-[10px] text-muted-foreground-faint">
                  {d.meta.lastQueryAt ? relativeTime(d.meta.lastQueryAt) : ""}
                </span>
              </button>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
