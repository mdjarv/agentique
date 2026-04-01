import { useNavigate } from "@tanstack/react-router";
import { Send, User, Users } from "lucide-react";
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
import { type TimelineEvent, broadcastToTeam, getTeamTimeline } from "~/lib/team-actions";
import { cn, getErrorMessage } from "~/lib/utils";
import type { SessionState } from "~/stores/chat-store";
import { useChatStore } from "~/stores/chat-store";
import { useTeamStore } from "~/stores/team-store";

interface ChannelPanelProps {
  projectSlug: string;
  channelId: string;
}

const EMPTY_TIMELINE: TimelineEvent[] = [];
const FALLBACK_COLOR: AgentColor = AGENT_COLORS[0] ?? {
  bg: "bg-amber-500/20",
  text: "text-amber-400",
  border: "border-amber-500/20",
  from: "from-amber-500/12",
  to: "to-amber-500/6",
};

export const ChannelPanel = memo(function ChannelPanel({
  projectSlug,
  channelId,
}: ChannelPanelProps) {
  const ws = useWebSocket();
  const navigate = useNavigate();
  const team = useTeamStore((s) => s.teams[channelId]);
  const timeline = useTeamStore((s) => s.timelines[channelId] ?? EMPTY_TIMELINE);
  const scrollRef = useRef<HTMLDivElement>(null);
  const [membersRef] = useAutoAnimate<HTMLDivElement>(ANIMATE_DEFAULT);
  useMergedAutoAnimate(scrollRef, ANIMATE_DEFAULT);

  // Load timeline on mount
  useEffect(() => {
    getTeamTimeline(ws, channelId)
      .then((events) => useTeamStore.getState().setTimeline(channelId, events))
      .catch((err) => toast.error(getErrorMessage(err, "Failed to load timeline")));
  }, [ws, channelId]);

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

  // Assign colors by member index
  const colorBySession = useMemo(() => {
    const map = new Map<string, AgentColor>();
    for (let i = 0; i < (team?.members.length ?? 0); i++) {
      const m = team?.members[i];
      if (m) map.set(m.sessionId, AGENT_COLORS[i % AGENT_COLORS.length] ?? FALLBACK_COLOR);
    }
    return map;
  }, [team?.members]);

  const [message, setMessage] = useState("");
  const [sending, setSending] = useState(false);

  const handleBroadcast = useCallback(async () => {
    if (!message.trim()) return;
    setSending(true);
    try {
      await broadcastToTeam(ws, channelId, message.trim());
      setMessage("");
    } catch (err) {
      toast.error(getErrorMessage(err, "Failed to send message"));
    } finally {
      setSending(false);
    }
  }, [ws, channelId, message]);

  const handleMemberClick = useCallback(
    (sessionId: string) => {
      navigate({
        to: "/project/$projectSlug/session/$sessionShortId",
        params: { projectSlug, sessionShortId: sessionId.split("-")[0] ?? "" },
      });
    },
    [navigate, projectSlug],
  );

  if (!team) return null;

  return (
    <div className="flex flex-col h-full">
      {/* Member list */}
      <div className="shrink-0 border-b px-3 py-2">
        <div className="flex items-center gap-1.5 text-xs text-muted-foreground mb-1.5">
          <Users className="h-3 w-3" />
          <span className="font-medium">{team.name || "Channel"}</span>
          <span>({team.members.length})</span>
        </div>
        <div ref={membersRef} className="flex flex-col gap-1">
          {team.members.map((member) => (
            <MemberRow
              key={member.sessionId}
              sessionId={member.sessionId}
              name={member.name}
              role={member.role}
              memberState={member.state}
              memberConnected={member.connected}
              color={colorBySession.get(member.sessionId) ?? FALLBACK_COLOR}
              onClick={handleMemberClick}
            />
          ))}
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
          const senderSessionData = useChatStore.getState().sessions[event.senderSessionId];
          const SenderIcon = getSessionIconComponent(senderSessionData?.meta.icon);
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
      <div className="shrink-0 border-t px-3 py-2">
        <div className="flex gap-1.5">
          <textarea
            value={message}
            onChange={(e) => setMessage(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey) {
                e.preventDefault();
                handleBroadcast();
              }
            }}
            placeholder="Broadcast to all members..."
            rows={1}
            className="flex-1 text-xs bg-background border rounded px-2 py-1.5 outline-none focus:ring-1 focus:ring-ring resize-none"
          />
          <Button
            size="sm"
            variant="ghost"
            className="h-7 px-2 self-end"
            disabled={!message.trim() || sending}
            onClick={handleBroadcast}
          >
            <Send className="h-3.5 w-3.5" />
          </Button>
        </div>
      </div>
    </div>
  );
});

// Extracted member row to read per-session store data without re-rendering the whole panel
interface MemberRowProps {
  sessionId: string;
  name: string;
  role: string;
  memberState: string;
  memberConnected: boolean;
  color: AgentColor;
  onClick: (sessionId: string) => void;
}

const MemberRow = memo(function MemberRow({
  sessionId,
  name,
  role,
  memberState,
  memberConnected,
  color,
  onClick,
}: MemberRowProps) {
  const sessionData = useChatStore((s) => s.sessions[sessionId]);
  const state = (sessionData?.meta.state ?? memberState) as SessionState;
  const connected = sessionData?.meta.connected ?? memberConnected;
  const Icon = getSessionIconComponent(sessionData?.meta.icon);

  return (
    <button
      type="button"
      onClick={() => onClick(sessionId)}
      className="flex items-center gap-2 text-xs px-1 py-0.5 rounded hover:bg-accent/50 w-full text-left cursor-pointer"
    >
      <Icon className={cn("h-3.5 w-3.5 shrink-0", color.text)} />
      <span className="truncate">{name || "Unnamed"}</span>
      {role && <span className="text-muted-foreground truncate">{role}</span>}
      <span className="ml-auto shrink-0">
        <SessionStatusBadge
          state={state}
          connected={connected}
          hasPendingApproval={sessionData?.pendingApproval != null}
          isPlanning={sessionData?.planMode}
        />
      </span>
    </button>
  );
});
