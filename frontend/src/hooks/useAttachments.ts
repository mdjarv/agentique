import type { ChangeEvent, ClipboardEvent } from "react";
import { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import {
  isAllowedType,
  isImage,
  MAX_ATTACHMENT_BYTES,
  MAX_ATTACHMENTS,
} from "~/lib/composer-constants";
import { readFileAsDataUrl, uuid } from "~/lib/utils";
import type { Attachment } from "~/stores/chat-store";

export function useAttachments() {
  const [attachments, setAttachments] = useState<Attachment[]>([]);
  const [lightboxSrc, setLightboxSrc] = useState<string | null>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const attachmentsRef = useRef(attachments);
  attachmentsRef.current = attachments;

  useEffect(() => {
    return () => {
      for (const a of attachmentsRef.current) {
        if (a.previewUrl) URL.revokeObjectURL(a.previewUrl);
      }
    };
  }, []);

  // Stable: it is handed to the window drop target, and reads only refs.
  const addFiles = useCallback(async (files: File[]) => {
    const allowed = files.filter((f) => isAllowedType(f.type));
    if (allowed.length === 0) {
      // Silent refusal is fine for a picker filtered by `accept` and for a
      // paste (which checks before it hijacks the event), but a drop can be
      // anything the desktop holds, and it lands nowhere visible.
      toast.error("Only images and PDFs can be attached");
      return;
    }

    const remaining = MAX_ATTACHMENTS - attachmentsRef.current.length;
    if (remaining <= 0) {
      toast.error(`Maximum ${MAX_ATTACHMENTS} attachments per message`);
      return;
    }
    const batch = allowed.slice(0, remaining);
    if (batch.length < allowed.length) {
      toast.warning(`Only ${remaining} more attachment(s) allowed`);
    }

    const added: Attachment[] = [];
    for (const file of batch) {
      if (file.size > MAX_ATTACHMENT_BYTES) {
        toast.error(`${file.name} exceeds 5 MB limit`);
        continue;
      }
      try {
        const dataUrl = await readFileAsDataUrl(file);
        added.push({
          id: uuid(),
          name: file.name,
          mimeType: file.type,
          dataUrl,
          previewUrl: isImage(file.type) ? URL.createObjectURL(file) : undefined,
        });
      } catch {
        toast.error(`Failed to read ${file.name}`);
      }
    }
    if (added.length > 0) {
      setAttachments((prev) => [...prev, ...added]);
    }
  }, []);

  const removeAttachment = (id: string) => {
    setAttachments((prev) => {
      const a = prev.find((i) => i.id === id);
      if (a?.previewUrl) URL.revokeObjectURL(a.previewUrl);
      return prev.filter((i) => i.id !== id);
    });
  };

  const clearAll = () => {
    setAttachments((prev) => {
      for (const a of prev) {
        if (a.previewUrl) URL.revokeObjectURL(a.previewUrl);
      }
      return [];
    });
  };

  const handlePaste = (e: ClipboardEvent) => {
    const files = Array.from(e.clipboardData.files);
    if (files.length === 0) return;
    const hasAllowed = files.some((f) => isAllowedType(f.type));
    if (!hasAllowed) return;
    e.preventDefault();
    addFiles(files);
  };

  const handleFileInput = (e: ChangeEvent<HTMLInputElement>) => {
    const files = Array.from(e.target.files ?? []);
    if (files.length > 0) addFiles(files);
    e.target.value = "";
  };

  // Dropping is not handled here. A file dragged into the app is destined for
  // the next message wherever it is released, so the drop target is the window
  // — see `lib/file-drop.ts`. This hook only supplies `addFiles` as its sink.

  return {
    attachments,
    lightboxSrc,
    setLightboxSrc,
    fileInputRef,
    addFiles,
    removeAttachment,
    clearAll,
    handlePaste,
    handleFileInput,
  };
}
