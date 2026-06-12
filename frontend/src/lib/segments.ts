import type { AgentMessageType } from "~/lib/channel-actions";
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

const CHANNEL_SEND_TOOL = "mcp__agentique-channel__SendMessage";

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
  timestamp?: number;
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
  deliveryStatus?: "sending" | "delivered" | "queued";
  timestamp?: number;
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
export interface ChannelSendSegment {
  kind: "channel_send";
  to: string;
  message: string;
  messageType?: AgentMessageType;
  toolId: string;
}

export type Segment =
  | ActivitySegment
  | TextSegment
  | ErrorSegment
  | CompactSegment
  | UserMessageSegment
  | AgentMessageSegment
  | ChannelSendSegment;
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
    case "turn_diff":
    case "tool_output_delta":
    case "reasoning_delta":
    case "tool_progress":
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
  // Tracks the single tool item per toolId so duplicate tool_use events (e.g. a
  // codex pending-approval tool_use followed by the started item, both keyed on
  // the same item ID) merge instead of producing orphan rows, and so results
  // correlate in O(1). Distinct codex fileChange files use "{itemID}#N" IDs and
  // therefore stay as separate items.
  const toolItemsById = new Map<string, Extract<ActivityItem, { kind: "tool" }>>();

  // First pass: collect task events indexed by parent toolUseId,
  // and identify channel-send tool IDs so we can suppress their results.
  const taskEventsByToolUseId = new Map<string, TaskEvent[]>();
  const channelSendToolIds = new Set<string>();
  for (const event of events) {
    if (event.type === "task" && event.toolUseId) {
      let list = taskEventsByToolUseId.get(event.toolUseId);
      if (!list) {
        list = [];
        taskEventsByToolUseId.set(event.toolUseId, list);
      }
      list.push(event);
    }
    if (event.type === "tool_use" && event.toolName === CHANNEL_SEND_TOOL) {
      channelSendToolIds.add(event.toolId);
    }
  }

  for (const event of events) {
    const kind = classifyEvent(event);
    if (event.type === "result") {
      resultEvent = event;
      continue;
    }
    if (kind === "skip") continue;

    // Intercept channel SendMessage tool_use → channel_send segment
    if (event.type === "tool_use" && event.toolName === CHANNEL_SEND_TOOL) {
      const input = event.toolInput as { to?: string; message?: string; type?: string } | null;
      segments.push({
        kind: "channel_send",
        to: input?.to ?? "",
        message: input?.message ?? "",
        messageType: (input?.type as AgentMessageType) ?? undefined,
        toolId: event.toolId,
      });
      continue;
    }

    const last = segments[segments.length - 1];

    // tool_result: suppress results for channel sends; otherwise attach to its
    // tool_use by ID. Codex fileChange items fan out to one tool_use + tool_result
    // per changed file ("{itemID}#N"), each correlating by its own suffixed ID.
    if (event.type === "tool_result") {
      if (channelSendToolIds.has(event.toolId)) continue;
      const item = toolItemsById.get(event.toolId);
      if (item) item.result = event;
      continue;
    }

    // tool_use: dedupe by ID. A pending-approval tool_use and the subsequent
    // started/completed tool_use share the same item ID (codex), so they merge
    // into one element rather than appearing as a separate orphan row; the later
    // event's input/name wins.
    if (event.type === "tool_use") {
      const existing = toolItemsById.get(event.toolId);
      if (existing) {
        existing.use = event;
        existing.taskEvents ??= taskEventsByToolUseId.get(event.toolId);
        continue;
      }
      const item: Extract<ActivityItem, { kind: "tool" }> = {
        kind: "tool",
        use: event,
        taskEvents: taskEventsByToolUseId.get(event.toolId),
      };
      toolItemsById.set(event.toolId, item);
      if (last?.kind === "activity") {
        last.items.push(item);
      } else {
        segments.push({ kind: "activity", items: [item] });
      }
      continue;
    }

    if (last?.kind === kind) {
      switch (last.kind) {
        case "activity":
          // tool_use is handled (with dedupe) before this switch; only thinking
          // items reach here.
          if (event.type === "thinking") {
            last.items.push({ kind: "thinking", event });
          }
          break;
        case "text":
          if (event.type === "text") {
            last.content += `\n\n${event.content}`;
            if (event.timestamp != null) last.timestamp = event.timestamp;
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
          // tool_use is handled (with dedupe) before this switch; only thinking
          // items reach here.
          if (event.type === "thinking") {
            segments.push({ kind: "activity", items: [{ kind: "thinking", event }] });
          }
          break;
        case "text":
          if (event.type === "text") {
            segments.push({
              kind: "text",
              content: event.content,
              timestamp: event.timestamp,
            });
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
          // Queued messages (next-turn delivery) live only in the streaming
          // buffer, never in committed turn events — guard anyway.
          if (event.type === "user_message") {
            if (
              (event.deliveryStatus === "sending" || event.deliveryStatus === "queued") &&
              !turnComplete
            )
              break;
            segments.push({
              kind: "user_message",
              content: event.content ?? "",
              attachments: event.attachments,
              deliveryStatus: event.deliveryStatus,
              timestamp: event.timestamp,
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
    case "channel_send":
      return `ch-send-${seg.toolId}`;
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
