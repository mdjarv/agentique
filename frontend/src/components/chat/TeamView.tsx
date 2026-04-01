import { Send, Trash2, User, Users } from "lucide-react";
import { type UIEvent, memo, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import { AGENT_COLORS, type AgentColor } from "~/components/chat/AgentMessage";
import { Markdown } from "~/components/chat/Markdown";
import { SessionStatusBadge } from "~/components/layout/SessionStatusBadge";
import { Avatar, AvatarFallback } from "~/components/ui/avatar";
import { Button } from "~/components/ui/button";
import { ANIMATE_DEFAULT, useAutoAnimate, useMergedAutoAnimate } from "~/hooks/useAutoAnimate";
import { useWebSocket } from "~/hooks/useWebSocket";
import { getSessionIconComponent } from "~/lib/session-icons";
import {
  type TimelineEvent,
  dissolveTeam,
  getTeamTimeline,
  sendTeamMessage,
} from "~/lib/team-actions";
import { cn, getErrorMessage } from "~/lib/utils";
import type { SessionMetadata } from "~/stores/chat-store";
import { useTeamStore } from "~/stores/team-store";

interface TeamViewProps {
  sessionId: string;
  teamId: string;
  sessions: Record<string, { meta: SessionMetadata }>;
}

const EMPTY_TIMELINE: TimelineEvent[] = [];
const FALLBACK_COLOR: AgentColor = AGENT_COLORS[0] ?? {
  bg: "bg-amber-500/20",
  text: "text-amber-400",
  border: "border-amber-500/20",
  from: "from-amber-500/12",
  to: "to-amber-500/6",
};

export const TeamView = memo(function TeamView({ sessionId, teamId, sessions }: TeamViewProps) {
  const ws = useWebSocket();
  const team = useTeamStore((s) => s.teams[teamId]);
  const timeline = useTeamStore((s) => s.timelines[teamId] ?? EMPTY_TIMELINE);
  const [targetId, setTargetId] = useState<string>("");
  const [message, setMessage] = useState("");
  const [sending, setSending] = useState(false);
  const scrollRef = useRef<HTMLDivElement>(null);
  const [membersRef] = useAutoAnimate<HTMLDivElement>(ANIMATE_DEFAULT);
  useMergedAutoAnimate(scrollRef, ANIMATE_DEFAULT);

  // Load timeline on mount
  useEffect(() => {
    getTeamTimeline(ws, teamId)
      .then((events) => useTeamStore.getState().setTimeline(teamId, events))
      .catch(() => {});
  }, [ws, teamId]);

  // Auto-scroll only when already at bottom
  const wasAtBottomRef = useRef(true);
  const timelineLen = timeline.length;

  const handleScroll = useCallback((e: UIEvent<HTMLDivElement>) => {
    const el = e.currentTarget;
    wasAtBottomRef.current = el.scrollTop + el.clientHeight >= el.scrollHeight - 40;
  }, []);

  useEffect(() => {
    if (timelineLen > 0 && wasAtBottomRef.current) {
      scrollRef.current?.scrollTo({ top: scrollRef.current.scrollHeight, behavior: "smooth" });
    }
  }, [timelineLen]);

  const handleSend = useCallback(async () => {
    if (!message.trim() || !targetId) return;
    setSending(true);
    try {
      await sendTeamMessage(ws, sessionId, targetId, message.trim());
      setMessage("");
    } catch (err) {
      toast.error(getErrorMessage(err, "Failed to send message"));
    } finally {
      setSending(false);
    }
  }, [ws, sessionId, targetId, message]);

  const [dissolving, setDissolving] = useState(false);
  const handleDissolve = useCallback(async () => {
    if (!teamId) return;
    setDissolving(true);
    try {
      await dissolveTeam(ws, teamId);
    } catch (err) {
      toast.error(getErrorMessage(err, "Failed to dissolve team"));
    } finally {
      setDissolving(false);
    }
  }, [ws, teamId]);

  // Assign colors by member index to guarantee no duplicates
  const colorBySession = useMemo(() => {
    const map = new Map<string, (typeof AGENT_COLORS)[number]>();
    for (let i = 0; i < (team?.members.length ?? 0); i++) {
      const m = team?.members[i];
      if (m) map.set(m.sessionId, AGENT_COLORS[i % AGENT_COLORS.length] ?? FALLBACK_COLOR);
    }
    return map;
  }, [team?.members]);

  if (!team) return null;

  const allMembers = team.members;

  return (
    <div className="flex flex-col h-full">
      {/* Member list */}
      <div className="shrink-0 border-b px-3 py-2">
        <div className="flex items-center gap-1.5 text-xs text-muted-foreground mb-1.5">
          <Users className="h-3 w-3" />
          <span className="font-medium">{team.name || "Team"}</span>
          <span>({team.members.length})</span>
          <Button
            size="sm"
            variant="ghost"
            className="ml-auto h-5 px-1.5 text-muted-foreground hover:text-destructive"
            disabled={dissolving}
            onClick={handleDissolve}
            title="Dissolve team — stop and delete all workers"
          >
            <Trash2 className="h-3 w-3" />
          </Button>
        </div>
        <div ref={membersRef} className="flex flex-col gap-1">
          {team.members.map((member) => {
            const color = colorBySession.get(member.sessionId) ?? FALLBACK_COLOR;
            const sessionData = sessions[member.sessionId];
            const state = sessionData?.meta.state ?? member.state;
            const Icon = getSessionIconComponent(sessionData?.meta.icon);
            return (
              <div
                key={member.sessionId}
                className="flex items-center gap-2 text-xs px-1 py-0.5 rounded"
              >
                <Icon className={cn("h-3.5 w-3.5 shrink-0", color.text)} />
                <span className="truncate">{member.name || "Unnamed"}</span>
                {member.role && (
                  <span className="text-muted-foreground truncate">{member.role}</span>
                )}
                <span className="ml-auto shrink-0">
                  <SessionStatusBadge
                    state={state as import("~/stores/chat-store").SessionState}
                    connected={sessionData?.meta.connected ?? member.connected}
                  />
                </span>
              </div>
            );
          })}
        </div>
      </div>

      {/* Timeline */}
      <div
        ref={scrollRef}
        onScroll={handleScroll}
        className="flex-1 overflow-y-auto px-3 py-2 space-y-2"
      >
        {timeline.length === 0 && (
          <p className="text-xs text-muted-foreground text-center py-4">No messages yet</p>
        )}
        {timeline.map((event, i) => {
          const key = `${event.senderSessionId}-${i}`;

          if (event.fromUser) {
            return (
              <div key={key} className="flex gap-3 flex-row-reverse">
                <Avatar className="h-8 w-8 shrink-0">
                  <AvatarFallback className="bg-primary/20 text-primary">
                    <User className="h-4 w-4" />
                  </AvatarFallback>
                </Avatar>
                <div className="max-w-[80%]">
                  <div className="rounded-lg px-4 py-2 text-xs bg-gradient-to-br from-primary/18 to-primary/10 border border-primary/15 shadow-lg shadow-black/30">
                    <Markdown content={event.content} />
                  </div>
                </div>
              </div>
            );
          }

          const color = colorBySession.get(event.senderSessionId) ?? FALLBACK_COLOR;
          const senderMeta = sessions[event.senderSessionId]?.meta;
          const SenderIcon = getSessionIconComponent(senderMeta?.icon);
          return (
            <div key={key} className="flex gap-3">
              <Avatar className="h-8 w-8 shrink-0">
                <AvatarFallback className={cn(color.bg, color.text)}>
                  <SenderIcon className="h-4 w-4" />
                </AvatarFallback>
              </Avatar>
              <div className="max-w-[80%]">
                <span className={cn("text-[10px] font-medium block mb-0.5", color.text)}>
                  {event.senderName}
                </span>
                <div
                  className={`rounded-lg px-4 py-2 text-xs bg-gradient-to-br ${color.from} ${color.to} shadow-lg shadow-black/30 border ${color.border}`}
                >
                  <Markdown content={event.content} />
                </div>
              </div>
            </div>
          );
        })}
      </div>

      {/* Composer */}
      <div className="shrink-0 border-t px-3 py-2 space-y-1.5">
        <select
          value={targetId}
          onChange={(e) => setTargetId(e.target.value)}
          className="w-full text-xs bg-background border rounded px-2 py-1"
        >
          <option value="">Send to...</option>
          {allMembers.map((m) => (
            <option key={m.sessionId} value={m.sessionId}>
              {m.name || "Unnamed"}
              {m.role && ` (${m.role})`}
            </option>
          ))}
        </select>
        <div className="flex gap-1.5">
          <input
            value={message}
            onChange={(e) => setMessage(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey) {
                e.preventDefault();
                handleSend();
              }
            }}
            placeholder="Message..."
            className="flex-1 text-xs bg-background border rounded px-2 py-1.5 outline-none focus:ring-1 focus:ring-ring"
          />
          <Button
            size="sm"
            variant="ghost"
            className="h-7 px-2"
            disabled={!message.trim() || !targetId || sending}
            onClick={handleSend}
          >
            <Send className="h-3.5 w-3.5" />
          </Button>
        </div>
      </div>
    </div>
  );
});
