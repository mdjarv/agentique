import { FileText, X } from "lucide-react";
import { memo } from "react";
import { isImage } from "~/lib/composer-constants";
import type { Attachment } from "~/stores/chat-store";

interface AttachmentStripProps {
  attachments: Attachment[];
  onRemove: (id: string) => void;
  onPreview: (src: string) => void;
}

/**
 * Thumbnail strip above the composer. Memoized so it only re-renders when the
 * attachment set changes — not on every keystroke.
 */
export const AttachmentStrip = memo(function AttachmentStrip({
  attachments,
  onRemove,
  onPreview,
}: AttachmentStripProps) {
  if (attachments.length === 0) return null;

  return (
    <div className="flex gap-2 flex-wrap mb-2">
      {attachments.map((a) => (
        <div key={a.id} className="relative group">
          {isImage(a.mimeType) ? (
            <button
              type="button"
              className="p-0 border-none bg-transparent cursor-pointer"
              onClick={() => onPreview(a.previewUrl ?? a.dataUrl)}
            >
              <img
                src={a.previewUrl ?? a.dataUrl}
                alt={a.name}
                className="h-16 w-16 object-cover rounded-md border"
              />
            </button>
          ) : (
            <div className="h-16 w-16 rounded-md border bg-muted flex flex-col items-center justify-center gap-1 px-1">
              <FileText className="h-5 w-5 text-muted-foreground" />
              <span className="text-[9px] text-muted-foreground truncate w-full text-center">
                {a.name}
              </span>
            </div>
          )}
          <button
            type="button"
            onClick={() => onRemove(a.id)}
            className="absolute -top-1.5 -right-1.5 h-4 w-4 rounded-full bg-destructive text-destructive-foreground flex items-center justify-center max-md:opacity-100 opacity-0 group-hover:opacity-100 transition-opacity"
          >
            <X className="h-2.5 w-2.5" />
          </button>
        </div>
      ))}
    </div>
  );
});
