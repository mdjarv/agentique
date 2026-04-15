import type {
  Attachment,
  ChatEvent,
  CompactBoundaryEvent,
  ErrorEvent,
  ResultEvent,
  TaskEvent,
  ThinkingEvent,
  ToolResultEvent,
  ToolUseEvent,
} from "~/stores/chat-types";

// --- Segment types ---

export type ActivityItem =
  | { kind: "thinking"; event: ThinkingEvent }
  | { kind: "tool"; use: ToolUseEvent; result?: ToolResultEvent; taskEvents?: TaskEvent[] };

export interface ActivitySegment {
  kind: "activity";
  items: ActivityItem[];
}
export interface TextSegment {
  kind: "text";
  content: string;
}
export interface ErrorSegment {
  kind: "error";
  events: ErrorEvent[];
}
export interface CompactSegment {
  kind: "compact";
  event: CompactBoundaryEvent;
}
export interface UserMessageSegment {
  kind: "user_message";
  content: string;
  attachments?: Attachment[];
  deliveryStatus?: "sending" | "delivered";
}
export interface AgentMessageSegment {
  kind: "agent_message";
  direction: "sent" | "received";
  content: string;
  messageType?: "plan" | "progress" | "done" | "message";
  senderName: string;
  senderSessionId: string;
  senderIcon?: string;
  targetName: string;
  targetSessionId: string;
  targetIcon?: string;
}

export type Segment =
  | ActivitySegment
  | TextSegment
  | ErrorSegment
  | CompactSegment
  | UserMessageSegment
  | AgentMessageSegment;
export type SegmentKind = Segment["kind"];

// --- Classification ---

export function classifyEvent(e: ChatEvent): SegmentKind | "result" | "skip" {
  switch (e.type) {
    case "thinking":
    case "tool_use":
    case "tool_result":
      return "activity";
    case "text":
      return "text";
    case "error":
      return "error";
    case "result":
      return "result";
    case "compact_boundary":
      return "compact";
    case "user_message":
      return "user_message";
    case "agent_message":
      return "agent_message";
    case "task":
      return "skip";
    default:
      return "skip";
  }
}

// --- Segment building ---

export function buildSegments(
  events: ChatEvent[],
  turnComplete: boolean,
): { segments: Segment[]; resultEvent?: ResultEvent } {
  const segments: Segment[] = [];
  let resultEvent: ResultEvent | undefined;

  // First pass: collect task events indexed by parent toolUseId
  const taskEventsByToolUseId = new Map<string, TaskEvent[]>();
  for (const event of events) {
    if (event.type === "task" && event.toolUseId) {
      let list = taskEventsByToolUseId.get(event.toolUseId);
      if (!list) {
        list = [];
        taskEventsByToolUseId.set(event.toolUseId, list);
      }
      list.push(event);
    }
  }

  for (const event of events) {
    const kind = classifyEvent(event);
    if (event.type === "result") {
      resultEvent = event;
      continue;
    }
    if (kind === "skip") continue;

    const last = segments[segments.length - 1];

    // tool_result: find matching tool_use in any segment (may cross segment boundaries).
    if (event.type === "tool_result") {
      for (let s = segments.length - 1; s >= 0; s--) {
        const seg = segments[s];
        if (seg?.kind !== "activity") continue;
        const item = seg.items.find((it) => it.kind === "tool" && it.use.toolId === event.toolId);
        if (item?.kind === "tool") {
          item.result = event;
          break;
        }
      }
      continue;
    }

    if (last?.kind === kind) {
      switch (last.kind) {
        case "activity":
          if (event.type === "thinking") {
            last.items.push({ kind: "thinking", event });
          } else if (event.type === "tool_use") {
            last.items.push({
              kind: "tool",
              use: event,
              taskEvents: taskEventsByToolUseId.get(event.toolId),
            });
          }
          break;
        case "text":
          if (event.type === "text") {
            last.content += `\n\n${event.content}`;
          }
          break;
        case "error":
          if (event.type === "error") {
            last.events.push(event);
          }
          break;
      }
    } else {
      switch (kind) {
        case "activity":
          if (event.type === "thinking") {
            segments.push({ kind: "activity", items: [{ kind: "thinking", event }] });
          } else if (event.type === "tool_use") {
            segments.push({
              kind: "activity",
              items: [
                {
                  kind: "tool",
                  use: event,
                  taskEvents: taskEventsByToolUseId.get(event.toolId),
                },
              ],
            });
          }
          break;
        case "text":
          if (event.type === "text") {
            segments.push({ kind: "text", content: event.content });
          }
          break;
        case "error":
          if (event.type === "error") {
            segments.push({ kind: "error", events: [event] });
          }
          break;
        case "compact":
          if (event.type === "compact_boundary") {
            segments.push({ kind: "compact", event });
          }
          break;
        case "user_message":
          // Pending messages render pinned at the bottom of MessageList.
          // Once the turn completes, treat any remaining "sending" as delivered.
          if (event.type === "user_message") {
            if (event.deliveryStatus === "sending" && !turnComplete) break;
            segments.push({
              kind: "user_message",
              content: event.content ?? "",
              attachments: event.attachments,
              deliveryStatus: event.deliveryStatus,
            });
          }
          break;
        case "agent_message":
          if (event.type === "agent_message") {
            segments.push({
              kind: "agent_message",
              direction: event.direction ?? "received",
              content: event.content ?? "",
              messageType: event.messageType,
              senderName: event.senderName ?? "",
              senderSessionId: event.senderSessionId ?? "",
              targetName: event.targetName ?? "",
              targetSessionId: event.targetSessionId ?? "",
            });
          }
          break;
      }
    }
  }

  return { segments, resultEvent };
}

// --- Keys ---

export function segmentKey(seg: Segment, i: number): string {
  switch (seg.kind) {
    case "activity": {
      const first = seg.items[0];
      if (!first) return `seg-${i}`;
      return (first.kind === "thinking" ? first.event.id : first.use.id) ?? `seg-${i}`;
    }
    case "error":
      return seg.events[0]?.id ?? `seg-${i}`;
    case "text":
      return `text-${i}`;
    case "compact":
      return seg.event.id ?? `compact-${i}`;
    case "user_message":
      return `user-msg-${i}`;
    case "agent_message":
      return `agent-msg-${i}`;
  }
}

// --- Turn section grouping ---

export type TurnSection =
  | { kind: "agent"; items: { seg: Segment; idx: number }[] }
  | { kind: "user"; seg: UserMessageSegment; idx: number };

export function buildTurnSections(segments: Segment[]): TurnSection[] {
  const sections: TurnSection[] = [];
  let run: { seg: Segment; idx: number }[] = [];
  segments.forEach((seg, idx) => {
    if (seg.kind === "user_message") {
      if (run.length > 0) sections.push({ kind: "agent", items: run });
      run = [];
      sections.push({ kind: "user", seg, idx });
    } else {
      run.push({ seg, idx });
    }
  });
  if (run.length > 0) sections.push({ kind: "agent", items: run });
  return sections;
}
