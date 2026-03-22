import { FileText, Globe, Search, Terminal } from "lucide-react";

interface ToolUseBlockProps {
  name: string;
  input: unknown;
}

function getToolIcon(name: string) {
  switch (name) {
    case "Read":
      return <FileText className="h-3 w-3" />;
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

function formatInput(name: string, input: unknown): string {
  if (typeof input === "string") return input;
  if (!input || typeof input !== "object") return JSON.stringify(input, null, 2);

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
    default:
      return JSON.stringify(input, null, 2);
  }
}

export function ToolUseBlock({ name, input }: ToolUseBlockProps) {
  const formatted = formatInput(name, input);

  return (
    <div className="border rounded-md bg-muted/50 text-xs">
      <div className="flex items-center gap-2 px-2 py-1.5 text-muted-foreground">
        {getToolIcon(name)}
        <span className="font-medium">{name}</span>
        <span className="text-muted-foreground/70 truncate">{formatted}</span>
      </div>
    </div>
  );
}
