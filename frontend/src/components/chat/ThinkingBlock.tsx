import { useState } from "react";
import { ExpandableRow } from "~/components/chat/ExpandableRow";
import { ThinkingIcon } from "~/components/chat/ToolIcons";

interface ThinkingBlockProps {
  content: string;
}

function previewLine(content: string): string {
  const line = content.trimStart().split("\n")[0] ?? "";
  return line.length > 120 ? `${line.slice(0, 120)}...` : line;
}

export function ThinkingBlock({ content }: ThinkingBlockProps) {
  const [expanded, setExpanded] = useState(false);
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
}
