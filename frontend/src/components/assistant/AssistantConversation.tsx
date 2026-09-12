import { Phone, Sparkles, User } from "lucide-react";
import { memo } from "react";
import { Markdown } from "~/components/chat/Markdown";
import { Avatar, AvatarFallback } from "~/components/ui/avatar";
import { Tooltip, TooltipContent, TooltipTrigger } from "~/components/ui/tooltip";
import type { AssistantMessage } from "~/lib/assistant/wire";
import { cn } from "~/lib/utils";

/**
 * The conversation: the operator's turns and the assistant's, in the order they
 * were said.
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
 * it back.
 */

interface AssistantConversationProps {
  messages: AssistantMessage[];
  /** The head's reply in progress, or null. Rendered as the last turn. */
  streaming: string | null;
}

export const AssistantConversation = memo(function AssistantConversation({
  messages,
  streaming,
}: AssistantConversationProps) {
  return (
    <div className="flex flex-col gap-4 px-3 py-4 md:px-6">
      {messages.map((message, index) => (
        <MessageRow key={message.id ?? `${message.createdAt ?? ""}-${index}`} message={message} />
      ))}
      {streaming !== null && <StreamingRow text={streaming} />}
    </div>
  );
});

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
          "min-w-0 rounded-lg border px-3 py-2 text-sm",
          fromUser
            ? "max-w-[75%] max-md:max-w-full bg-primary/10 border-primary/15"
            : "flex-1 bg-agent/5 border-agent/15",
        )}
      >
        {message.surface === "voice" && <VoiceMark callId={message.callId} />}
        <Markdown content={message.text ?? ""} preserveNewlines={fromUser} />
      </div>
    </div>
  );
});

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
