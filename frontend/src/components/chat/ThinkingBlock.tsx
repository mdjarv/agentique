import { memo, useState } from "react";
import { ExpandableRow } from "~/components/chat/ExpandableRow";
import { ThinkingIcon } from "~/components/chat/ToolIcons";

interface ThinkingBlockProps {
  content: string;
  signature?: string;
}

function previewLine(content: string): string {
  const line = content.trimStart().split("\n")[0] ?? "";
  return line.length > 120 ? `${line.slice(0, 120)}...` : line;
}

export const ThinkingBlock = memo(function ThinkingBlock({
  content,
  signature,
}: ThinkingBlockProps) {
  const [expanded, setExpanded] = useState(false);
  const redacted = content.length === 0 && (signature?.length ?? 0) > 0;

  if (redacted) {
    return (
      <div className="border rounded-md bg-muted/50 px-3 py-1.5 flex items-center gap-2 text-xs text-muted-foreground italic">
        <ThinkingIcon className="shrink-0" />
        <span className="truncate">Thinking (hidden by Opus 4.7)</span>
      </div>
    );
  }

  const preview = previewLine(content);

  return (
    <div className="border rounded-md bg-muted/50">
      <ExpandableRow expanded={expanded} onToggle={() => setExpanded(!expanded)}>
        <ThinkingIcon className="shrink-0" />
        <span className="truncate italic">{expanded ? "Thinking" : preview || "Thinking"}</span>
      </ExpandableRow>
      {expanded && (
        <div className="px-3 pb-2 text-xs text-muted-foreground italic whitespace-pre-wrap">
          {content}
        </div>
      )}
    </div>
  );
});
