import { fromWireAttachment } from "~/lib/attachment-utils";
import { uuid } from "~/lib/utils";
import type {
  AgentMessageEvent,
  AgentResultEvent,
  ChatEvent,
  ChatEventType,
  CompactBoundaryEvent,
  CompactStatusEvent,
  ContextManagementEvent,
  ErrorEvent,
  MessageDeliveryEvent,
  RateLimitEvent,
  ResultEvent,
  StreamEvent,
  TaskEvent,
  TextEvent,
  ThinkingEvent,
  ToolContentBlock,
  ToolResultEvent,
  ToolUseEvent,
  UserMessageEvent,
} from "~/stores/chat-types";

/** All recognized ChatEvent type values. */
const KNOWN_EVENT_TYPES = new Set<ChatEventType>([
  "text",
  "thinking",
  "tool_use",
  "tool_result",
  "result",
  "error",
  "rate_limit",
  "stream",
  "compact_status",
  "compact_boundary",
  "context_management",
  "user_message",
  "message_delivery",
  "agent_message",
  "agent_result",
  "task",
]);

function isKnownEventType(value: unknown): value is ChatEventType {
  return typeof value === "string" && KNOWN_EVENT_TYPES.has(value as ChatEventType);
}

/** Parse a raw server event (wire format) into a typed ChatEvent. Returns undefined for unknown types. */
export function parseServerEvent(raw: Record<string, unknown>): ChatEvent | undefined {
  if (!isKnownEventType(raw.type)) {
    console.warn("[events] Unknown event type, skipping:", raw.type);
    return undefined;
  }

  const id = typeof raw.id === "string" ? raw.id : uuid();
  const timestamp = raw.timestamp as number | undefined;
  const parentToolUseId = raw.parentToolUseId as string | undefined;

  switch (raw.type) {
    case "text":
      return {
        id,
        type: "text",
        content: (raw.content as string) ?? "",
        timestamp,
        parentToolUseId,
      } satisfies TextEvent;

    case "thinking":
      return {
        id,
        type: "thinking",
        content: (raw.content as string) ?? "",
        timestamp,
        parentToolUseId,
      } satisfies ThinkingEvent;

    case "tool_use":
      return {
        id,
        type: "tool_use",
        toolId: (raw.toolId as string) ?? "",
        toolName: (raw.toolName as string) ?? "",
        toolInput: raw.toolInput,
        category: raw.category as string | undefined,
        timestamp,
        parentToolUseId,
      } satisfies ToolUseEvent;

    case "tool_result":
      return {
        id,
        type: "tool_result",
        toolId: (raw.toolId as string) ?? "",
        contentBlocks: raw.content as ToolContentBlock[] | undefined,
        timestamp,
        parentToolUseId,
      } satisfies ToolResultEvent;

    case "result":
      return {
        id,
        type: "result",
        cost: raw.cost as number | undefined,
        duration: raw.duration as number | undefined,
        usage: raw.usage as { inputTokens: number; outputTokens: number } | undefined,
        stopReason: raw.stopReason as string | undefined,
        contextWindow: raw.contextWindow as number | undefined,
        inputTokens: raw.inputTokens as number | undefined,
        outputTokens: raw.outputTokens as number | undefined,
        timestamp,
        parentToolUseId,
      } satisfies ResultEvent;

    case "error":
      return {
        id,
        type: "error",
        content: (raw.content as string) ?? "",
        fatal: raw.fatal as boolean | undefined,
        errorType: raw.errorType as string | undefined,
        retryAfterSecs: raw.retryAfterSecs as number | undefined,
        timestamp,
        parentToolUseId,
      } satisfies ErrorEvent;

    case "rate_limit":
      return {
        id,
        type: "rate_limit",
        status: raw.status as string | undefined,
        utilization: raw.utilization as number | undefined,
        resetsAt: raw.resetsAt as number | undefined,
        rateLimitType: raw.rateLimitType as string | undefined,
        timestamp,
        parentToolUseId,
      } satisfies RateLimitEvent;

    case "stream":
      return { id, type: "stream", timestamp, parentToolUseId } satisfies StreamEvent;

    case "compact_status":
      return {
        id,
        type: "compact_status",
        status: raw.status as string | undefined,
        timestamp,
        parentToolUseId,
      } satisfies CompactStatusEvent;

    case "compact_boundary":
      return {
        id,
        type: "compact_boundary",
        trigger: raw.trigger as string | undefined,
        preTokens: raw.preTokens as number | undefined,
        timestamp,
        parentToolUseId,
      } satisfies CompactBoundaryEvent;

    case "context_management":
      return {
        id,
        type: "context_management",
        timestamp,
        parentToolUseId,
      } satisfies ContextManagementEvent;

    case "user_message":
      return {
        id,
        type: "user_message",
        content: raw.content as string | undefined,
        fromUser: raw.fromUser as boolean | undefined,
        messageId: raw.messageId as string | undefined,
        attachments: Array.isArray(raw.attachments)
          ? raw.attachments.map((a) => {
              const obj = a != null && typeof a === "object" ? (a as Record<string, unknown>) : {};
              return fromWireAttachment({
                name: typeof obj.name === "string" ? obj.name : "",
                mimeType: typeof obj.mimeType === "string" ? obj.mimeType : "",
                dataUrl: typeof obj.dataUrl === "string" ? obj.dataUrl : "",
              });
            })
          : undefined,
        timestamp,
        parentToolUseId,
      } satisfies UserMessageEvent;

    case "message_delivery":
      return {
        id,
        type: "message_delivery",
        messageId: raw.messageId as string | undefined,
        deliveryStatus: raw.deliveryStatus as "sending" | "delivered" | undefined,
        timestamp,
        parentToolUseId,
      } satisfies MessageDeliveryEvent;

    case "agent_message":
      return {
        id,
        type: "agent_message",
        direction: raw.direction as "sent" | "received" | undefined,
        fromUser: raw.fromUser as boolean | undefined,
        senderSessionId: raw.senderSessionId as string | undefined,
        senderName: raw.senderName as string | undefined,
        targetSessionId: raw.targetSessionId as string | undefined,
        targetName: raw.targetName as string | undefined,
        content: raw.content as string | undefined,
        messageType: raw.messageType as "plan" | "progress" | "done" | "message" | undefined,
        timestamp,
        parentToolUseId,
      } satisfies AgentMessageEvent;

    case "task":
      return {
        id,
        type: "task",
        toolUseId: raw.toolUseId as string | undefined,
        taskSubtype: raw.taskSubtype as TaskEvent["taskSubtype"],
        taskDescription: raw.taskDescription as string | undefined,
        taskType: raw.taskType as string | undefined,
        taskSummary: raw.taskSummary as string | undefined,
        taskStatus: raw.taskStatus as string | undefined,
        lastToolName: raw.lastToolName as string | undefined,
        totalTokens: raw.totalTokens as number | undefined,
        toolUses: raw.toolUses as number | undefined,
        durationMs: raw.durationMs as number | undefined,
        timestamp,
        parentToolUseId,
      } satisfies TaskEvent;

    case "agent_result":
      return {
        id,
        type: "agent_result",
        status: raw.status as string | undefined,
        agentId: raw.agentId as string | undefined,
        agentType: raw.agentType as string | undefined,
        contentBlocks: raw.content as ToolContentBlock[] | undefined,
        totalDurationMs: raw.totalDurationMs as number | undefined,
        totalTokens: raw.totalTokens as number | undefined,
        totalToolUseCount: raw.totalToolUseCount as number | undefined,
        timestamp,
        parentToolUseId,
      } satisfies AgentResultEvent;
  }
}
