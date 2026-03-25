import { Brain, ChevronDown, ChevronRight } from "lucide-react";
import { useState } from "react";

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
      <button
        type="button"
        onClick={() => setExpanded(!expanded)}
        className="flex items-center gap-2 p-2 text-xs text-muted-foreground w-full text-left hover:bg-muted/80 transition-colors min-w-0"
      >
        {expanded ? (
          <ChevronDown className="h-3 w-3 shrink-0" />
        ) : (
          <ChevronRight className="h-3 w-3 shrink-0" />
        )}
        <Brain className="h-3 w-3 shrink-0" />
        <span className="truncate italic">{expanded ? "Thinking" : preview || "Thinking"}</span>
      </button>
      {expanded && (
        <div className="px-3 pb-2 text-xs text-muted-foreground italic whitespace-pre-wrap">
          {content}
        </div>
      )}
    </div>
  );
}
