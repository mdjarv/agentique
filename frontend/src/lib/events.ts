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
    category: raw.category as string | undefined,
    errorType: raw.errorType as string | undefined,
    retryAfterSecs: raw.retryAfterSecs as number | undefined,
    trigger: raw.trigger as string | undefined,
    preTokens: raw.preTokens as number | undefined,
    attachments: type === "user_message" ? parseAttachments(raw.attachments) : undefined,
    senderSessionId: raw.senderSessionId as string | undefined,
    senderName: raw.senderName as string | undefined,
  };
}
