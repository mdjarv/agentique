import { uuid } from "~/lib/utils";
import type { Attachment, ChatEvent, ToolContentBlock } from "~/stores/chat-store";

function parseAttachments(raw: unknown): Attachment[] | undefined {
  if (!Array.isArray(raw) || raw.length === 0) return undefined;
  return raw.map((a) => ({
    id: uuid(),
    name: (a as Record<string, string>).name ?? "",
    mimeType: (a as Record<string, string>).mimeType ?? "",
    dataUrl: (a as Record<string, string>).dataUrl ?? "",
  }));
}

/** Parse a raw server event (wire format) into a ChatEvent. */
export function parseServerEvent(raw: Record<string, unknown>): ChatEvent {
  const type = raw.type as ChatEvent["type"];
  return {
    id: uuid(),
    type,
    content: type !== "tool_result" ? (raw.content as string | undefined) : undefined,
    contentBlocks:
      type === "tool_result" ? (raw.content as ToolContentBlock[] | undefined) : undefined,
    toolId: raw.toolId as string | undefined,
    toolName: raw.toolName as string | undefined,
    toolInput: raw.toolInput,
    cost: raw.cost as number | undefined,
    duration: raw.duration as number | undefined,
    usage: raw.usage as { inputTokens: number; outputTokens: number } | undefined,
    stopReason: raw.stopReason as string | undefined,
    contextWindow: raw.contextWindow as number | undefined,
    inputTokens: raw.inputTokens as number | undefined,
    outputTokens: raw.outputTokens as number | undefined,
    fatal: raw.fatal as boolean | undefined,
    status: raw.status as string | undefined,
    utilization: raw.utilization as number | undefined,
    resetsAt: raw.resetsAt as number | undefined,
    rateLimitType: raw.rateLimitType as string | undefined,
    category: raw.category as string | undefined,
    errorType: raw.errorType as string | undefined,
    retryAfterSecs: raw.retryAfterSecs as number | undefined,
    trigger: raw.trigger as string | undefined,
    preTokens: raw.preTokens as number | undefined,
    messageId: raw.messageId as string | undefined,
    attachments: type === "user_message" ? parseAttachments(raw.attachments) : undefined,
    direction: raw.direction as "sent" | "received" | undefined,
    senderSessionId: raw.senderSessionId as string | undefined,
    senderName: raw.senderName as string | undefined,
    targetSessionId: raw.targetSessionId as string | undefined,
    targetName: raw.targetName as string | undefined,
    // Subagent task fields
    toolUseId: raw.toolUseId as string | undefined,
    taskSubtype: raw.taskSubtype as ChatEvent["taskSubtype"],
    taskDescription: raw.taskDescription as string | undefined,
    taskType: raw.taskType as string | undefined,
    taskSummary: raw.taskSummary as string | undefined,
    taskStatus: raw.taskStatus as string | undefined,
    lastToolName: raw.lastToolName as string | undefined,
    totalTokens: raw.totalTokens as number | undefined,
    toolUses: raw.toolUses as number | undefined,
    durationMs: raw.durationMs as number | undefined,
    parentToolUseId: raw.parentToolUseId as string | undefined,
  };
}
