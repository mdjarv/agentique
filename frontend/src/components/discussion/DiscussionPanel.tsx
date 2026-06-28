import { useNavigate } from "@tanstack/react-router";
import { Loader2, SendHorizonal, Square } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import { Markdown } from "~/components/chat/Markdown";
import { Button } from "~/components/ui/button";
import { Textarea } from "~/components/ui/textarea";
import { useWebSocket } from "~/hooks/useWebSocket";
import { type ChannelMessage, getChannelTimeline } from "~/lib/channel-actions";
import { discussionRound, stopDiscussion } from "~/lib/discussion-actions";
import { cn, getErrorMessage } from "~/lib/utils";
import { useChannelStore } from "~/stores/channel-store";
import { useDiscussionStore } from "~/stores/discussion-store";

const EMPTY_TIMELINE: ChannelMessage[] = [];

// Full Tailwind class names (no dynamic interpolation) so the persona palette
// survives the JIT. Indexed by the persona's position in the roster.
const PERSONA_COLORS = [
  { text: "text-primary", dot: "bg-primary", border: "border-l-primary" },
  { text: "text-orange", dot: "bg-orange", border: "border-l-orange" },
  { text: "text-teal", dot: "bg-teal", border: "border-l-teal" },
  { text: "text-agent", dot: "bg-agent", border: "border-l-agent" },
  { text: "text-info", dot: "bg-info", border: "border-l-info" },
  { text: "text-warning", dot: "bg-warning", border: "border-l-warning" },
  { text: "text-success", dot: "bg-success", border: "border-l-success" },
  { text: "text-destructive", dot: "bg-destructive", border: "border-l-destructive" },
] as const;

export function DiscussionPanel({ channelId }: { channelId: string }) {
  const ws = useWebSocket();
  const navigate = useNavigate();
  const discussion = useDiscussionStore((s) => s.discussions[channelId]);
  const timeline = useChannelStore((s) => s.timelines[channelId] ?? EMPTY_TIMELINE);

  const [prompt, setPrompt] = useState("");
  const [sending, setSending] = useState(false);
  const scrollRef = useRef<HTMLDivElement>(null);

  // Seed the timeline + subscribe to live persona contributions. appendTimelineEvent
  // dedups by id, so a global channel.message subscription overlapping this one is safe.
  useEffect(() => {
    let cancelled = false;
    getChannelTimeline(ws, channelId)
      .then((msgs) => {
        if (!cancelled) useChannelStore.getState().setTimeline(channelId, msgs);
      })
      .catch((err) => console.error("getChannelTimeline failed", err));
    const unsub = ws.subscribe("channel.message", (payload) => {
      const msg = payload as ChannelMessage;
      if (msg.channelId === channelId) {
        useChannelStore.getState().appendTimelineEvent(channelId, msg);
      }
    });
    return () => {
      cancelled = true;
      unsub();
    };
  }, [ws, channelId]);

  // Map each persona name → a stable color index from the roster.
  const colorOf = useMemo(() => {
    const map = new Map<string, (typeof PERSONA_COLORS)[number]>();
    (discussion?.personas ?? []).forEach((name, i) => {
      map.set(name, PERSONA_COLORS[i % PERSONA_COLORS.length] ?? PERSONA_COLORS[0]);
    });
    return map;
  }, [discussion?.personas]);

  // biome-ignore lint/correctness/useExhaustiveDependencies: scroll on new messages
  useEffect(() => {
    scrollRef.current?.scrollTo({ top: scrollRef.current.scrollHeight, behavior: "smooth" });
  }, [timeline.length]);

  const running = discussion?.running ?? false;

  const sendRound = useCallback(async () => {
    const text = prompt.trim();
    if (!text || sending || running) return;
    setSending(true);
    try {
      await discussionRound(ws, { channelId, prompt: text });
      setPrompt("");
    } catch (err) {
      toast.error(getErrorMessage(err, "Failed to send round"));
    } finally {
      setSending(false);
    }
  }, [prompt, sending, running, ws, channelId]);

  const stop = useCallback(async () => {
    try {
      await stopDiscussion(ws, { channelId });
      navigate({ to: "/discussions" });
    } catch (err) {
      toast.error(getErrorMessage(err, "Failed to stop discussion"));
    }
  }, [ws, channelId, navigate]);

  if (!discussion) {
    return (
      <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
        This discussion is no longer active.
      </div>
    );
  }

  return (
    <div className="grid h-full grid-cols-[240px_1fr]">
      {/* Roster rail */}
      <aside className="flex flex-col border-r border-border bg-sidebar overflow-y-auto">
        <div className="p-4">
          <div className="text-sm font-semibold text-foreground">{discussion.groupName}</div>
          <div className="mt-1 flex flex-wrap items-center gap-1.5 text-[11px] text-muted-foreground">
            <span className="rounded-full border border-teal/30 bg-teal/10 px-2 py-0.5 text-teal">
              {discussion.scope === "repo-backed" ? "◧ repo-backed" : "🌐 web-only"}
            </span>
            <span>{discussion.mode}</span>
          </div>
          {discussion.worktreeBranch && (
            <div className="mt-1 font-mono text-[10px] text-muted-foreground/70">
              {discussion.worktreeBranch}
            </div>
          )}
        </div>
        <div className="px-3 pb-1 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground/60">
          Cast · {discussion.personas.length}
        </div>
        <div className="flex flex-col gap-1 px-2">
          {discussion.personas.map((name) => {
            const c = colorOf.get(name) ?? PERSONA_COLORS[0];
            return (
              <div
                key={name}
                className="flex items-center gap-2.5 rounded-lg border border-border/60 bg-card px-2.5 py-2"
              >
                <span
                  className={cn(
                    "grid size-7 shrink-0 place-items-center rounded-lg text-xs font-bold text-background",
                    c.dot,
                  )}
                >
                  {name.slice(0, 2)}
                </span>
                <span className={cn("truncate text-sm font-semibold", c.text)}>{name}</span>
              </div>
            );
          })}
        </div>
        <div className="mt-auto p-3">
          <Button variant="outline" size="sm" className="w-full text-destructive" onClick={stop}>
            <Square className="size-3.5" />
            Stop &amp; keep transcript
          </Button>
        </div>
      </aside>

      {/* Transcript + composer */}
      <main className="flex min-w-0 flex-col">
        <div className="flex items-center gap-2 border-b border-border px-5 py-2.5 text-xs text-muted-foreground">
          <span className="font-semibold text-foreground">Round {discussion.round}</span>
          <span>· {discussion.mode}</span>
          {running && (
            <span className="ml-auto flex items-center gap-1.5 text-teal">
              <Loader2 className="size-3 animate-spin" /> personas responding…
            </span>
          )}
        </div>

        <div ref={scrollRef} className="flex-1 overflow-y-auto px-5 py-4">
          <div className="mx-auto flex w-full max-w-3xl flex-col gap-4">
            {timeline.length === 0 && (
              <div className="py-12 text-center text-sm text-muted-foreground">
                The opening round is starting…
              </div>
            )}
            {timeline.map((msg) => {
              const fromUser = msg.senderType === "user";
              const c = colorOf.get(msg.senderName) ?? PERSONA_COLORS[0];
              return (
                <div key={msg.id} className="flex gap-3">
                  <span
                    className={cn(
                      "mt-0.5 grid size-8 shrink-0 place-items-center rounded-lg text-xs font-bold",
                      fromUser ? "bg-muted text-muted-foreground" : cn(c.dot, "text-background"),
                    )}
                  >
                    {fromUser ? "You" : msg.senderName.slice(0, 2)}
                  </span>
                  <div className="min-w-0 flex-1">
                    <div
                      className={cn(
                        "mb-1 text-sm font-semibold",
                        fromUser ? "text-foreground" : c.text,
                      )}
                    >
                      {fromUser ? "You" : msg.senderName}
                    </div>
                    <div
                      className={cn(
                        "rounded-lg border border-border/60 bg-card px-3.5 py-2.5 text-sm",
                        !fromUser && cn("border-l-2", c.border),
                      )}
                    >
                      <Markdown content={msg.content} />
                    </div>
                  </div>
                </div>
              );
            })}
          </div>
        </div>

        <div className="border-t border-border p-4">
          <div className="mx-auto flex w-full max-w-3xl items-end gap-2">
            <Textarea
              value={prompt}
              onChange={(e) => setPrompt(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && !e.shiftKey) {
                  e.preventDefault();
                  void sendRound();
                }
              }}
              placeholder={running ? "A round is in progress…" : "Send the next round…"}
              className="min-h-[44px] resize-none text-sm"
              disabled={running || sending}
            />
            <Button
              size="sm"
              disabled={running || sending || prompt.trim() === ""}
              onClick={() => void sendRound()}
            >
              {sending ? (
                <Loader2 className="size-3.5 animate-spin" />
              ) : (
                <SendHorizonal className="size-3.5" />
              )}
              Send round
            </Button>
          </div>
        </div>
      </main>
    </div>
  );
}
