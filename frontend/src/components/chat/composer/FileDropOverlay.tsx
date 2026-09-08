import { Upload } from "lucide-react";
import { createPortal } from "react-dom";
import { MAX_ATTACHMENT_BYTES, MAX_ATTACHMENTS } from "~/lib/composer-constants";

const MAX_MB = Math.round(MAX_ATTACHMENT_BYTES / (1024 * 1024));

/**
 * The affordance for a window-wide drop. Two things it must be:
 *
 * `pointer-events-none`, because the drop is handled by window listeners and an
 * overlay that took the pointer would become the drag's own target — appearing
 * under the cursor fires a `dragleave` for whatever was there, and the overlay
 * would flicker itself out of existence.
 *
 * Portalled and above the `z-50` dialog layer, because it accepts a drop from
 * wherever the app is, including with a sheet open over it.
 */
export function FileDropOverlay({ visible }: { visible: boolean }) {
  if (!visible) return null;

  return createPortal(
    <div className="pointer-events-none fixed inset-0 z-[60] flex items-center justify-center bg-background/60 p-6 backdrop-blur-[2px]">
      <div className="flex flex-col items-center gap-2 rounded-2xl border-2 border-dashed border-agent bg-card/95 px-8 py-6 shadow-lg">
        <Upload className="h-6 w-6 text-agent" />
        <p className="text-sm font-medium">Drop to attach</p>
        <p className="text-xs text-muted-foreground">
          Images and PDFs · up to {MAX_ATTACHMENTS} files, {MAX_MB} MB each
        </p>
      </div>
    </div>,
    document.body,
  );
}
