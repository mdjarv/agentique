import { Loader2 } from "lucide-react";
import { memo, useState } from "react";
import { ExpandableRow } from "~/components/chat/ExpandableRow";
import { ThinkingIcon } from "~/components/chat/ToolIcons";
import { useDebouncedValue } from "~/hooks/useDebouncedValue";

interface ThinkingBlockProps {
  content: string;
  signature?: string;
  isStreaming?: boolean;
}

function previewLine(content: string): string {
  const line = content.trimStart().split("\n")[0] ?? "";
  return line.length > 120 ? `${line.slice(0, 120)}...` : line;
}

export const ThinkingBlock = memo(function ThinkingBlock({
  content,
  signature,
  isStreaming,
}: ThinkingBlockProps) {
  const [expanded, setExpanded] = useState(false);
  const redacted = content.length === 0 && (signature?.length ?? 0) > 0;
  const debouncedContent = useDebouncedValue(content, 80);
  const displayContent = isStreaming ? debouncedContent : content;

  if (redacted) {
    return (
      <div className="border rounded-md bg-muted/50 px-3 py-1.5 flex items-center gap-2 text-xs text-muted-foreground italic">
        <ThinkingIcon className="shrink-0" />
        <span className="truncate">Thinking (hidden by Opus 4.8)</span>
      </div>
    );
  }

  const preview = previewLine(displayContent);
  const showExpanded = expanded || isStreaming;

  return (
    <div className="border rounded-md bg-muted/50">
      <ExpandableRow expanded={!!showExpanded} onToggle={() => setExpanded(!expanded)}>
        {isStreaming ? (
          <Loader2 className="h-3 w-3 animate-spin shrink-0" />
        ) : (
          <ThinkingIcon className="shrink-0" />
        )}
        <span className="truncate italic">{showExpanded ? "Thinking" : preview || "Thinking"}</span>
      </ExpandableRow>
      {showExpanded && (
        <div className="px-3 pb-2 text-xs text-muted-foreground italic whitespace-pre-wrap">
          {displayContent}
        </div>
      )}
    </div>
  );
});
