import { useNavigate } from "@tanstack/react-router";
import { Archive, Hash, SendHorizonal, Trash2, User, Users } from "lucide-react";
import { memo, type UIEvent, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import { useShallow } from "zustand/react/shallow";
import { type AgentColor, getAgentColors } from "~/components/chat/AgentMessage";
import { Markdown } from "~/components/chat/Markdown";
import { PageHeader } from "~/components/layout/PageHeader";
import { SessionStatusBadge } from "~/components/layout/session/SessionStatusBadge";
import { Avatar, AvatarFallback } from "~/components/ui/avatar";
import { Popover, PopoverContent, PopoverTrigger } from "~/components/ui/popover";
import { ANIMATE_DEFAULT, useAutoAnimate, useMergedAutoAnimate } from "~/hooks/useAutoAnimate";
import { useTheme } from "~/hooks/useTheme";
import { useWebSocket } from "~/hooks/useWebSocket";
import {
  broadcastToChannel,
  dissolveChannel,
  dissolveChannelKeepHistory,
  getChannelTimeline,
  type TimelineEvent,
} from "~/lib/channel-actions";
import { getSessionIconComponent } from "~/lib/session/icons";
import { cn, getErrorMessage } from "~/lib/utils";
import { useChannelStore } from "~/stores/channel-store";
import type { SessionState } from "~/stores/chat-store";
import { useChatStore } from "~/stores/chat-store";

interface ChannelPanelProps {
  projectSlug: string;
  channelId: string;
}

interface TimelineGroup {
  senderId: string;
  senderName: string;
  fromUser: boolean;
  senderSessionId: string;
  events: TimelineEvent[];
}

function groupTimelineEvents(timeline: TimelineEvent[]): TimelineGroup[] {
  const groups: TimelineGroup[] = [];
  for (const event of timeline) {
    const senderId = event.fromUser ? "__user__" : event.senderSessionId;
    const lastGroup = groups[groups.length - 1];
    if (lastGroup && lastGroup.senderId === senderId) {
      lastGroup.events.push(event);
    } else {
      groups.push({
        senderId,
        senderName: event.fromUser ? "You" : event.senderName,
        fromUser: !!event.fromUser,
        senderSessionId: event.senderSessionId,
        events: [event],
      });
    }
  }
  return groups;
}

const EMPTY_TIMELINE: TimelineEvent[] = [];

export const ChannelPanel = memo(function ChannelPanel({
  projectSlug,
  channelId,
}: ChannelPanelProps) {
  const ws = useWebSocket();
  const { resolvedTheme } = useTheme();
  const navigate = useNavigate();
  const channel = useChannelStore((s) => s.channels[channelId]);
  const timeline = useChannelStore((s) => s.timelines[channelId] ?? EMPTY_TIMELINE);
  const scrollRef = useRef<HTMLDivElement>(null);
  const [membersRef] = useAutoAnimate<HTMLDivElement>(ANIMATE_DEFAULT);
  useMergedAutoAnimate(scrollRef, ANIMATE_DEFAULT);
  const [keepLoading, setKeepLoading] = useState(false);
  const [dissolveLoading, setDissolveLoading] = useState(false);

  const isArchived = useMemo(
    () => !!channel && channel.members.every((m) => m.role === "lead"),
    [channel],
  );

  const handleKeep = useCallback(async () => {
    setKeepLoading(true);
    try {
      await dissolveChannelKeepHistory(ws, channelId);
    } catch (err) {
      toast.error(getErrorMessage(err, "Failed to archive channel"));
    } finally {
      setKeepLoading(false);
    }
  }, [ws, channelId]);

  const handleDissolve = useCallback(async () => {
    setDissolveLoading(true);
    try {
      await dissolveChannel(ws, channelId);
    } catch (err) {
      toast.error(getErrorMessage(err, "Failed to dissolve channel"));
    } finally {
      setDissolveLoading(false);
    }
  }, [ws, channelId]);

  // Load timeline on mount
  useEffect(() => {
    getChannelTimeline(ws, channelId)
      .then((events) => useChannelStore.getState().setTimeline(channelId, events))
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
  const agentPalette = getAgentColors(resolvedTheme);
  const fallbackColor = agentPalette[0] as AgentColor;
  const colorBySession = useMemo(() => {
    const map = new Map<string, AgentColor>();
    for (let i = 0; i < (channel?.members.length ?? 0); i++) {
      const m = channel?.members[i];
      if (m) map.set(m.sessionId, agentPalette[i % agentPalette.length] as AgentColor);
    }
    return map;
  }, [channel?.members, agentPalette]);

  const groups = useMemo(() => groupTimelineEvents(timeline), [timeline]);

  // For each sender, find the index of their last group so we can show status there
  const lastGroupIndexBySender = useMemo(() => {
    const map = new Map<string, number>();
    for (let i = groups.length - 1; i >= 0; i--) {
      const g = groups[i];
      if (g && !map.has(g.senderId)) map.set(g.senderId, i);
    }
    return map;
  }, [groups]);

  // Reactive subscription for session icons — avoids getState() in render body
  const senderSessionIds = useMemo(() => {
    const ids = new Set<string>();
    for (const g of groups) {
      if (!g.fromUser) ids.add(g.senderSessionId);
    }
    return [...ids];
  }, [groups]);
  const sessionIcons = useChatStore(
    useShallow((s) => {
      const map: Record<string, string> = {};
      for (const id of senderSessionIds) {
        map[id] = s.sessions[id]?.meta.icon ?? "";
      }
      return map;
    }),
  );

  const [message, setMessage] = useState("");
  const [sending, setSending] = useState(false);

  const handleBroadcast = useCallback(async () => {
    if (!message.trim()) return;
    setSending(true);
    try {
      await broadcastToChannel(ws, channelId, message.trim());
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

  if (!channel) return null;

  return (
    <div className={cn("flex flex-col h-full", isArchived && "opacity-75")}>
      {/* Header */}
      <PageHeader>
        <Hash className="size-4 text-muted-foreground shrink-0" />
        <span className="font-medium truncate">{channel.name || "Channel"}</span>
        <span className="text-xs text-muted-foreground shrink-0">
          {channel.members.length} {channel.members.length === 1 ? "member" : "members"}
        </span>
        {isArchived && (
          <span className="text-[10px] text-muted-foreground italic ml-1">archived</span>
        )}

        <div className="ml-auto flex items-center gap-1">
          {/* Members popover */}
          <Popover>
            <PopoverTrigger asChild>
              <button
                type="button"
                title="View members"
                className="p-1.5 rounded hover:bg-accent/50 text-muted-foreground hover:text-foreground cursor-pointer"
              >
                <Users className="h-3.5 w-3.5" />
              </button>
            </PopoverTrigger>
            <PopoverContent align="end" className="w-56 p-2">
              <p className="text-xs font-medium text-muted-foreground mb-1.5 px-1">Members</p>
              <div ref={membersRef} className="flex flex-col gap-0.5">
                {channel.members.map((member) => (
                  <MemberRow
                    key={member.sessionId}
                    sessionId={member.sessionId}
                    name={member.name}
                    role={member.role}
                    memberState={member.state}
                    memberConnected={member.connected}
                    color={colorBySession.get(member.sessionId) ?? fallbackColor}
                    onClick={handleMemberClick}
                  />
                ))}
              </div>
            </PopoverContent>
          </Popover>

          {!isArchived && (
            <>
              <button
                type="button"
                onClick={handleKeep}
                disabled={keepLoading || dissolveLoading}
                title="Archive — stop workers, keep channel history"
                className="p-1.5 rounded hover:bg-accent/50 text-muted-foreground hover:text-foreground disabled:opacity-50 cursor-pointer"
              >
                <Archive className="h-3.5 w-3.5" />
              </button>
              <button
                type="button"
                onClick={handleDissolve}
                disabled={keepLoading || dissolveLoading}
                title="Dissolve — stop workers and delete channel"
                className="p-1.5 rounded hover:bg-destructive/20 text-muted-foreground hover:text-destructive disabled:opacity-50 cursor-pointer"
              >
                <Trash2 className="h-3.5 w-3.5" />
              </button>
            </>
          )}
        </div>
      </PageHeader>

      {/* Timeline */}
      <div ref={scrollRef} onScroll={handleScroll} className="flex-1 overflow-y-auto px-3 py-2">
        {timeline.length === 0 && (
          <p className="text-xs text-muted-foreground text-center py-4">No messages yet</p>
        )}
        {groups.map((group, gi) => {
          const color = group.fromUser
            ? { bg: "bg-primary/20", text: "text-primary", border: "", from: "", to: "" }
            : (colorBySession.get(group.senderSessionId) ?? fallbackColor);
          const SenderIcon = group.fromUser
            ? User
            : getSessionIconComponent(sessionIcons[group.senderSessionId]);
          const isLastForSender = lastGroupIndexBySender.get(group.senderId) === gi;

          return (
            <div key={`g-${group.senderId}-${gi}`} className={gi > 0 ? "mt-4" : ""}>
              {group.events.map((event, ei) => {
                const isFirst = ei === 0;
                if (isFirst) {
                  return (
                    <div
                      key={`${group.senderId}-${gi}-${ei}`}
                      className="flex gap-3 items-start -mx-3 px-3 py-0.5 rounded hover:bg-accent/20"
                    >
                      <Avatar className="h-8 w-8 shrink-0 mt-0.5">
                        <AvatarFallback className={cn(color.bg, color.text)}>
                          <SenderIcon className="h-4 w-4" />
                        </AvatarFallback>
                      </Avatar>
                      <div className="min-w-0 flex-1">
                        <span className="flex items-center gap-1.5">
                          <span className={cn("text-xs font-semibold", color.text)}>
                            {group.senderName}
                          </span>
                          {event.messageType && event.messageType !== "message" && (
                            <span className="text-[9px] uppercase tracking-wide text-muted-foreground/60 bg-foreground/5 px-1 py-px rounded">
                              {event.messageType}
                            </span>
                          )}
                          {isLastForSender && !group.fromUser && (
                            <SenderStatus sessionId={group.senderSessionId} />
                          )}
                        </span>
                        <div className="text-xs text-foreground">
                          <Markdown content={event.content} />
                        </div>
                      </div>
                    </div>
                  );
                }
                return (
                  <div
                    key={`${group.senderId}-${gi}-${ei}`}
                    className="-mx-3 px-3 py-0.5 rounded hover:bg-accent/20"
                  >
                    <div className="text-xs text-foreground pl-11">
                      {event.messageType && event.messageType !== "message" && (
                        <span className="text-[9px] uppercase tracking-wide text-muted-foreground/60 mr-1">
                          [{event.messageType}]
                        </span>
                      )}
                      <Markdown content={event.content} />
                    </div>
                  </div>
                );
              })}
            </div>
          );
        })}
      </div>

      {/* Composer */}
      {!isArchived && (
        <div className="shrink-0 border-t p-3">
          <div className="rounded-xl border bg-secondary/50 transition-all focus-within:border-ring/50 focus-within:ring-1 focus-within:ring-ring/30">
            <textarea
              value={message}
              onChange={(e) => setMessage(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
                  e.preventDefault();
                  handleBroadcast();
                }
              }}
              placeholder="Broadcast to all members..."
              rows={1}
              className="w-full resize-none bg-transparent px-3 pt-3 pb-1 text-sm placeholder:text-muted-foreground focus:outline-none"
              style={{ maxHeight: "200px" }}
            />
            <div className="flex items-center justify-end px-2 pb-2">
              <button
                type="button"
                onClick={handleBroadcast}
                disabled={!message.trim() || sending}
                className="h-8 w-8 rounded-lg bg-primary text-primary-foreground flex items-center justify-center transition-colors hover:bg-primary/90 disabled:opacity-30 disabled:cursor-not-allowed cursor-pointer"
                aria-label="Send message"
              >
                <SendHorizonal className="h-3.5 w-3.5" />
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
});

// Subscribes to a single session's state for showing inline status badges
const SenderStatus = memo(function SenderStatus({ sessionId }: { sessionId: string }) {
  const sessionData = useChatStore((s) => s.sessions[sessionId]);
  if (!sessionData) return null;
  const state = sessionData.meta.state as SessionState;
  return (
    <SessionStatusBadge
      state={state}
      connected={sessionData.meta.connected}
      hasPendingApproval={sessionData.pendingApproval != null}
      isPlanning={sessionData.planMode}
      className="size-3"
    />
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
