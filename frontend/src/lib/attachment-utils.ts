import { uuid } from "~/lib/utils";
import type { Attachment } from "~/stores/chat-types";

export interface WireAttachment {
  name: string;
  mimeType: string;
  dataUrl: string;
}

/** Strip previewUrl for wire transmission. */
export function toWireAttachment(a: Attachment): WireAttachment {
  return { name: a.name, mimeType: a.mimeType, dataUrl: a.dataUrl };
}

/** Add a client-side id from wire format. */
export function fromWireAttachment(a: WireAttachment): Attachment {
  return { id: uuid(), name: a.name, mimeType: a.mimeType, dataUrl: a.dataUrl };
}
