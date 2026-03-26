import { CheckCircle, ChevronDown, ChevronRight, Image } from "lucide-react";
import { useState } from "react";
import type { ToolContentBlock } from "~/stores/chat-store";

interface ToolResultBlockProps {
  content: ToolContentBlock[];
  onImageClick?: (src: string) => void;
}

export function ToolResultBlock({ content, onImageClick }: ToolResultBlockProps) {
  const [expanded, setExpanded] = useState(false);

  const textContent = content
    .filter((b) => b.type === "text")
    .map((b) => b.text ?? "")
    .join("");
  const images = content.filter((b) => b.type === "image");
  const lines = textContent.split("\n");
  const lineCount = lines.length;
  const preview = lines[0]?.slice(0, 80) ?? "";
  const hasImages = images.length > 0;

  return (
    <div className="border rounded-md bg-muted/50 text-xs">
      <button
        type="button"
        onClick={() => setExpanded(!expanded)}
        className="flex items-center gap-2 px-2 py-1.5 text-muted-foreground w-full text-left hover:bg-muted/80 transition-colors"
      >
        {expanded ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
        <CheckCircle className="h-3 w-3" />
        <span className="truncate">{expanded ? "Result" : preview || "Result"}</span>
        <span className="ml-auto flex items-center gap-1.5 text-muted-foreground/50 shrink-0">
          {hasImages && <Image className="h-3 w-3" />}
          {!expanded && lineCount > 1 && <span>{lineCount} lines</span>}
        </span>
      </button>
      {expanded && (
        <div className="border-t">
          {hasImages && (
            <div className="flex gap-2 flex-wrap p-2">
              {images.map((img) => (
                <button
                  key={img.url}
                  type="button"
                  className="p-0 border-none bg-transparent cursor-pointer"
                  onClick={() => img.url && onImageClick?.(img.url)}
                >
                  <img
                    src={img.url}
                    alt="Tool result"
                    className="max-h-64 max-w-full rounded border object-contain"
                  />
                </button>
              ))}
            </div>
          )}
          {textContent && (
            <pre className="p-2 overflow-x-auto text-foreground/80 whitespace-pre-wrap max-h-96 overflow-y-auto">
              {textContent}
            </pre>
          )}
        </div>
      )}
    </div>
  );
}
