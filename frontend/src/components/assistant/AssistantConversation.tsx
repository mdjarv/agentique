import { HeartPulse, Phone, Sparkles, User } from "lucide-react";
import { memo } from "react";
import { ProposalCard } from "~/components/assistant/ProposalCard";
import {
  EventFold,
  EventRow,
  TIMELINE_INDENT,
  UnseenDivider,
} from "~/components/assistant/TimelineEvents";
import { Markdown } from "~/components/chat/Markdown";
import { Avatar, AvatarFallback } from "~/components/ui/avatar";
import { Tooltip, TooltipContent, TooltipTrigger } from "~/components/ui/tooltip";
import { clockTime, type TimelineItem } from "~/lib/assistant/timeline";
import {
  type AssistantMessage,
  isHeartbeatNotice,
  isHeartbeatReply,
  isOpenProposal,
} from "~/lib/assistant/wire";
import { cn } from "~/lib/utils";

/**
 * The thread's one timeline: the operator's turns and the assistant's, the
 * journal's news and the proposal cards, in the order they happened
 * (`buildTimeline` decides the order and what folds).
 *
 * Built here rather than reusing `ChannelPanel`'s timeline, which is a
 * channel's rendering and says so — per-member colours, a member's live status
 * badge beside their last group, a session id behind every sender. This
 * conversation has two speakers and no members, and coupling it to that would
 * have meant inventing a membership to satisfy it. What IS reused is the thing
 * that has to be the same wherever you meet it: the chat's `Markdown`, so the
 * assistant's headings, lists and code blocks read as they do in a session.
 *
 * A turn said on a call carries `surface: "voice"` and wears a phone glyph —
 * the drive is in the thread, and where something was said is part of reading
 * it back. A turn the heartbeat started carries `kind: "heartbeat"`, and the
 * pair it arrives as renders as two different things: the server's note is a
 * divider (nobody said it), the head's reply an ordinary bubble with a mark.
 *
 * Everything that is not a turn sits in the bubbles' text column, so the eye
 * reads the avatars as the speakers and the indented lines as what happened
 * around them. A proposal card carries `data-proposal-id`, which is how the
 * page finds it to scroll to and to tell whether it is on screen.
 */

interface AssistantConversationProps {
  items: TimelineItem[];
  /** The head's reply in progress, or null. Rendered as the last turn. */
  streaming: string | null;
}

export const AssistantConversation = memo(function AssistantConversation({
  items,
  streaming,
}: AssistantConversationProps) {
  return (
    <div className="flex flex-col gap-4 px-3 py-4 md:px-6">
      {items.map((item) => (
        <TimelineRow key={item.key} item={item} />
      ))}
      {streaming !== null && <StreamingRow text={streaming} />}
    </div>
  );
});

const TimelineRow = memo(function TimelineRow({ item }: { item: TimelineItem }) {
  switch (item.type) {
    case "message":
      // The heartbeat's own note is not a turn anybody took, so it is not a
      // bubble. Everything else, the head's reply to it included, is.
      return isHeartbeatNotice(item.message) ? (
        <HeartbeatDivider message={item.message} />
      ) : (
        <MessageRow message={item.message} />
      );
    case "digest":
      return (
        <div className="flex flex-col gap-1">
          <MessageRow message={item.message} />
          {item.entries.length > 0 && (
            <div className={TIMELINE_INDENT}>
              <EventFold
                entries={item.entries}
                label={`Based on ${item.entries.length} ${item.entries.length === 1 ? "update" : "updates"}`}
              />
            </div>
          )}
        </div>
      );
    case "event":
      return (
        <ul className={cn("-my-2", TIMELINE_INDENT)}>
          <EventRow entry={item.entry} />
        </ul>
      );
    case "fold":
      return (
        <div className={cn("-my-2", TIMELINE_INDENT)}>
          <EventFold entries={item.entries} />
        </div>
      );
    case "proposal":
      return (
        <div
          className={cn(TIMELINE_INDENT, "max-w-3xl")}
          data-proposal-id={item.proposal.id}
          data-proposal-open={isOpenProposal(item.proposal.status) ? "" : undefined}
        >
          <ProposalCard proposal={item.proposal} />
        </div>
      );
    case "unseen":
      return <UnseenDivider />;
  }
});

/**
 * The heartbeat's wake-up note: one quiet line across the column.
 *
 * Not a bubble, because nobody said it — it is the server telling the
 * conversation that a tick found something worth acting on, and the sentence it
 * carries is the triage verdict. A bubble would put it in the head's voice, or
 * invent a third speaker; a rule with the sentence on it reads as what it is, a
 * seam in the conversation where a turn nobody typed begins.
 *
 * **The rule carries the first line and nothing else.** The stored message is
 * the verdict sentence AND the window the head was woken with — up to sixty
 * journal lines, which is the turn's own material and the thread's record of
 * what the assistant was told. Drawn whole, that made a multi-line blob inside a
 * horizontal rule; drawn as its first line, the divider says what a divider can
 * say. The rest stays reachable as the line's `title`, and the journal page is
 * where a window is read properly.
 *
 * It carries a clock time and not "3h ago": the whole point of the line is that
 * this happened while nobody was looking, so when is part of reading it.
 */
const HeartbeatDivider = memo(function HeartbeatDivider({
  message,
}: {
  message: AssistantMessage;
}) {
  const time = clockTime(message.createdAt);
  const full = message.text?.trim() ?? "";
  const verdict = firstLine(full) || "The heartbeat woke the assistant";
  return (
    <div className="flex items-center gap-2 py-1 text-muted-foreground-faint">
      <span aria-hidden className="h-px flex-1 bg-border/60" />
      <HeartPulse className="size-3 shrink-0" />
      <span className="min-w-0 truncate text-[11px] leading-snug" title={full || undefined}>
        {verdict}
      </span>
      {time && <span className="shrink-0 font-mono text-[10px] tabular-nums">{time}</span>}
      <span aria-hidden className="h-px w-4 bg-border/60" />
    </div>
  );
});

/**
 * The first line of a wake-up note: the verdict sentence.
 *
 * The server puts it first for exactly this, and the verdict itself can never be
 * more than one line — the triage parser refuses a multi-line answer — so the
 * cut is at the first newline and needs no other rule.
 */
function firstLine(text: string): string {
  const end = text.indexOf("\n");
  return (end === -1 ? text : text.slice(0, end)).trim();
}

const MessageRow = memo(function MessageRow({ message }: { message: AssistantMessage }) {
  const fromUser = message.role === "user";
  return (
    <div className={cn("flex gap-3 items-start", fromUser && "flex-row-reverse")}>
      <Avatar className="size-7 shrink-0">
        <AvatarFallback
          className={cn(fromUser ? "bg-primary/20 text-primary" : "bg-agent/20 text-agent")}
        >
          {fromUser ? <User className="size-3.5" /> : <Sparkles className="size-3.5" />}
        </AvatarFallback>
      </Avatar>
      <div
        className={cn(
          "min-w-0 overflow-x-auto rounded-lg border px-3 py-2 text-sm [overflow-wrap:anywhere]",
          fromUser
            ? "max-w-[75%] max-md:max-w-full bg-primary/10 border-primary/15"
            : "flex-1 bg-agent/5 border-agent/15",
        )}
      >
        {message.surface === "voice" && <VoiceMark callId={message.callId} />}
        {isHeartbeatReply(message) && <HeartbeatMark />}
        <Markdown content={message.text ?? ""} preserveNewlines={fromUser} />
      </div>
    </div>
  );
});

/**
 * Said without being asked. The bubble is ordinary — the head's reply to a
 * heartbeat is the same voice saying the same kind of thing — and only this small
 * word says it was not a reply to the operator. The same mark the timeline's
 * `heartbeat` journal entries wear, so one picture means one thing.
 */
function HeartbeatMark() {
  return (
    <span
      className="float-right ml-2 flex items-center gap-1 font-mono text-[10px] text-muted-foreground-faint"
      aria-label="Said by the heartbeat"
    >
      <HeartPulse className="size-3" />
      heartbeat
    </span>
  );
}

/** Said on a call. A mark, not a sentence — it is one bit about one turn. */
function VoiceMark({ callId }: { callId?: string }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className="float-right ml-2 text-muted-foreground" aria-label="Said on a voice call">
          <Phone className="size-3" />
        </span>
      </TooltipTrigger>
      <TooltipContent>{callId ? `Said on a call (${callId})` : "Said on a call"}</TooltipContent>
    </Tooltip>
  );
}

/**
 * The head's reply as it arrives.
 *
 * An empty one still draws: the ask is away and the gate is armed, so the row
 * is what says the assistant is thinking — the alternative is a page that looks
 * as though the send went nowhere for as long as the first token takes.
 */
const StreamingRow = memo(function StreamingRow({ text }: { text: string }) {
  return (
    <div className="flex gap-3 items-start">
      <Avatar className="size-7 shrink-0">
        <AvatarFallback className="bg-agent/20 text-agent">
          <Sparkles className="size-3.5 animate-pulse" />
        </AvatarFallback>
      </Avatar>
      <div
        className="min-w-0 flex-1 rounded-lg border border-dashed border-agent/25 bg-agent/5 px-3 py-2 text-sm"
        aria-live="polite"
        aria-busy
      >
        {text ? (
          <Markdown content={text} isStreaming />
        ) : (
          <span className="text-muted-foreground text-xs">Thinking…</span>
        )}
      </div>
    </div>
  );
});
