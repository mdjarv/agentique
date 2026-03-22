import { ChevronDown, ChevronRight, FileText, Globe, Pencil, Search, Terminal } from "lucide-react";
import { useState } from "react";

interface ToolUseBlockProps {
  name: string;
  input: unknown;
}

function getToolIcon(name: string) {
  switch (name) {
    case "Read":
      return <FileText className="h-3 w-3" />;
    case "Write":
    case "Edit":
      return <Pencil className="h-3 w-3" />;
    case "Glob":
    case "Grep":
      return <Search className="h-3 w-3" />;
    case "WebFetch":
    case "WebSearch":
      return <Globe className="h-3 w-3" />;
    default:
      return <Terminal className="h-3 w-3" />;
  }
}

function formatSummary(name: string, input: unknown): string {
  if (typeof input === "string") return input;
  if (!input || typeof input !== "object") return JSON.stringify(input);

  const obj = input as Record<string, unknown>;

  switch (name) {
    case "Read":
      return String(obj.file_path ?? "");
    case "Write":
      return String(obj.file_path ?? "");
    case "Edit":
      return String(obj.file_path ?? "");
    case "Glob":
      return String(obj.pattern ?? "");
    case "Grep":
      return `${obj.pattern ?? ""}${obj.path ? ` in ${obj.path}` : ""}`;
    case "Bash":
      return String(obj.command ?? obj.description ?? "");
    case "Agent":
      return String(obj.description ?? obj.prompt ?? "").slice(0, 120);
    default:
      return JSON.stringify(input).slice(0, 120);
  }
}

function formatDetail(name: string, input: unknown): string | null {
  if (!input || typeof input !== "object") return null;
  const obj = input as Record<string, unknown>;

  // Show full detail only when it adds info beyond the summary
  switch (name) {
    case "Agent":
      return JSON.stringify(input, null, 2);
    case "Bash":
      return obj.command ? String(obj.command) : null;
    case "Edit":
      return [
        obj.file_path && `File: ${obj.file_path}`,
        obj.old_string && `Old: ${String(obj.old_string).slice(0, 200)}`,
        obj.new_string && `New: ${String(obj.new_string).slice(0, 200)}`,
      ]
        .filter(Boolean)
        .join("\n");
    case "Grep":
      return JSON.stringify(input, null, 2);
    default: {
      const json = JSON.stringify(input, null, 2);
      return json.length > 100 ? json : null;
    }
  }
}

export function ToolUseBlock({ name, input }: ToolUseBlockProps) {
  const [expanded, setExpanded] = useState(false);
  const summary = formatSummary(name, input);
  const detail = formatDetail(name, input);
  const hasDetail = detail !== null;

  return (
    <div className="border rounded-md bg-muted/50 text-xs overflow-hidden">
      <button
        type="button"
        onClick={() => hasDetail && setExpanded(!expanded)}
        className={`flex items-center gap-2 px-2 py-1.5 text-muted-foreground w-full text-left min-w-0 ${hasDetail ? "hover:bg-muted/80 cursor-pointer" : ""} transition-colors`}
      >
        {hasDetail ? (
          expanded ? (
            <ChevronDown className="h-3 w-3 shrink-0" />
          ) : (
            <ChevronRight className="h-3 w-3 shrink-0" />
          )
        ) : (
          <span className="w-3 shrink-0" />
        )}
        {getToolIcon(name)}
        <span className="font-medium shrink-0">{name}</span>
        <span className="text-muted-foreground/70 truncate min-w-0">{summary}</span>
      </button>
      {expanded && detail && (
        <pre className="p-2 overflow-x-auto text-muted-foreground whitespace-pre-wrap border-t max-h-64 overflow-y-auto break-all">
          {detail}
        </pre>
      )}
    </div>
  );
}
