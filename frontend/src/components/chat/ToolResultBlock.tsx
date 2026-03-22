import { CheckCircle, ChevronDown, ChevronRight } from "lucide-react";
import { useState } from "react";

interface ToolResultBlockProps {
  content: string;
}

export function ToolResultBlock({ content }: ToolResultBlockProps) {
  const [expanded, setExpanded] = useState(false);
  const isLong = content.length > 500;
  const displayContent = !expanded && isLong ? `${content.slice(0, 500)}...` : content;

  return (
    <div className="border rounded-md bg-muted/50 text-xs">
      <button
        type="button"
        onClick={() => setExpanded(!expanded)}
        className="flex items-center gap-2 p-2 text-muted-foreground w-full text-left hover:bg-muted/80 transition-colors"
      >
        {isLong ? (
          expanded ? (
            <ChevronDown className="h-3 w-3" />
          ) : (
            <ChevronRight className="h-3 w-3" />
          )
        ) : (
          <CheckCircle className="h-3 w-3" />
        )}
        Result
      </button>
      <pre className="p-2 overflow-x-auto text-muted-foreground whitespace-pre-wrap">
        {displayContent}
      </pre>
    </div>
  );
}
