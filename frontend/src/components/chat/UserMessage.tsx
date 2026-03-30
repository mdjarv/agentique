import { Check, Copy, FileText, Loader2, User } from "lucide-react";
import { memo, useEffect, useState } from "react";
import { createPortal } from "react-dom";
import { Markdown } from "~/components/chat/Markdown";
import { Avatar, AvatarFallback } from "~/components/ui/avatar";
import { useCopyToClipboard } from "~/hooks/useCopyToClipboard";
import type { Attachment } from "~/stores/chat-store";

interface UserMessageProps {
  prompt: string;
  attachments?: Attachment[];
  deliveryStatus?: "sending" | "delivered";
}

export const UserMessage = memo(function UserMessage({
  prompt,
  attachments,
  deliveryStatus,
}: UserMessageProps) {
  const { copied, copy: handleCopy } = useCopyToClipboard();
  const [lightboxSrc, setLightboxSrc] = useState<string | null>(null);

  useEffect(() => {
    if (!lightboxSrc) return;
    const handleKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setLightboxSrc(null);
    };
    document.addEventListener("keydown", handleKey);
    return () => document.removeEventListener("keydown", handleKey);
  }, [lightboxSrc]);

  return (
    <>
      <div className="flex gap-3 flex-row-reverse max-md:flex-col max-md:items-end max-md:gap-1">
        <Avatar className="h-8 w-8 shrink-0 max-md:h-6 max-md:w-6">
          <AvatarFallback className="bg-primary/20 text-primary">
            <User className="h-4 w-4 max-md:h-3 max-md:w-3" />
          </AvatarFallback>
        </Avatar>
        <div className="group/usermsg relative max-w-[75%] max-md:max-w-full rounded-lg px-4 py-2 bg-gradient-to-br from-primary/20 to-primary/10 text-foreground shadow-lg shadow-black/30 border border-primary/10">
          {prompt && (
            <button
              type="button"
              onClick={() => handleCopy(prompt)}
              className="absolute -left-8 top-1 p-1 rounded opacity-0 group-hover/usermsg:opacity-100 hover:bg-muted text-muted-foreground transition-opacity z-10 max-md:static max-md:float-right max-md:opacity-60 max-md:ml-2 max-md:-mr-1 max-md:-mt-0.5"
              aria-label="Copy message"
            >
              {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
            </button>
          )}
          {attachments && attachments.length > 0 && (
            <div className="flex gap-1.5 flex-wrap mb-2">
              {attachments.map((a) =>
                a.mimeType.startsWith("image/") ? (
                  <button
                    key={a.id}
                    type="button"
                    className="p-0 border-none bg-transparent cursor-pointer"
                    onClick={() => setLightboxSrc(a.dataUrl)}
                  >
                    <img
                      src={a.previewUrl ?? a.dataUrl}
                      alt={a.name}
                      className="h-20 max-w-[200px] object-cover rounded"
                    />
                  </button>
                ) : (
                  <div
                    key={a.id}
                    className="h-20 w-20 rounded bg-primary-foreground/10 flex flex-col items-center justify-center gap-1 px-1"
                  >
                    <FileText className="h-5 w-5" />
                    <span className="text-[9px] truncate w-full text-center">{a.name}</span>
                  </div>
                ),
              )}
            </div>
          )}
          {prompt && <Markdown content={prompt} className="prose-user" preserveNewlines />}
          {deliveryStatus && (
            <div className="flex justify-end mt-1 -mb-0.5">
              {deliveryStatus === "sending" ? (
                <Loader2 className="h-3 w-3 text-muted-foreground/50 animate-spin" />
              ) : (
                <Check className="h-3 w-3 text-primary/60" />
              )}
            </div>
          )}
        </div>
      </div>

      {lightboxSrc &&
        createPortal(
          <dialog
            open
            className="fixed inset-0 z-50 bg-black/80 flex items-center justify-center cursor-pointer m-0 p-0 border-none max-w-none max-h-none w-screen h-screen"
            onClick={() => setLightboxSrc(null)}
            onKeyDown={(e) => {
              if (e.key === "Escape") setLightboxSrc(null);
            }}
            aria-label="Image preview"
          >
            <img
              src={lightboxSrc}
              alt="Full-size preview"
              className="max-h-[90vh] max-w-[90vw] object-contain rounded-lg"
            />
          </dialog>,
          document.body,
        )}
    </>
  );
});
