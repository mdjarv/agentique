import { uuid } from "~/lib/utils";
import type { ChatEvent } from "~/stores/chat-store";

/** Parse a raw server event (wire format) into a ChatEvent. */
export function parseServerEvent(raw: Record<string, unknown>): ChatEvent {
  return {
    id: uuid(),
    type: raw.type as ChatEvent["type"],
    content: raw.content as string | undefined,
    toolId: raw.toolId as string | undefined,
    toolName: raw.toolName as string | undefined,
    toolInput: raw.toolInput,
    cost: raw.cost as number | undefined,
    duration: raw.duration as number | undefined,
    usage: raw.usage as { inputTokens: number; outputTokens: number } | undefined,
    stopReason: raw.stopReason as string | undefined,
    fatal: raw.fatal as boolean | undefined,
  };
}
