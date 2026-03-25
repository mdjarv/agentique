import { CheckCircle, ChevronDown, ChevronRight } from "lucide-react";
import { useState } from "react";

interface ToolResultBlockProps {
  content: string;
}

export function ToolResultBlock({ content }: ToolResultBlockProps) {
  const [expanded, setExpanded] = useState(false);
  const lines = content.split("\n");
  const lineCount = lines.length;
  const preview = lines[0]?.slice(0, 80) ?? "";

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
        {!expanded && lineCount > 1 && (
          <span className="text-muted-foreground/50 ml-auto shrink-0">{lineCount} lines</span>
        )}
      </button>
      {expanded && (
        <pre className="p-2 overflow-x-auto text-foreground/80 whitespace-pre-wrap border-t max-h-96 overflow-y-auto">
          {content}
        </pre>
      )}
    </div>
  );
}
