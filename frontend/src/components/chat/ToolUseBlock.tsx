import {
  Bot,
  ChevronDown,
  ChevronRight,
  FileSearch,
  FileText,
  Globe,
  ListTodo,
  Pencil,
  PencilLine,
  Plug,
  Search,
  Terminal,
  Wrench,
} from "lucide-react";
import { useState } from "react";
import { Prism as SyntaxHighlighter } from "react-syntax-highlighter";
import { oneDark } from "react-syntax-highlighter/dist/esm/styles/prism";

interface ToolUseBlockProps {
  name: string;
  input: unknown;
  category?: string;
  projectPath?: string;
  worktreePath?: string;
}

function stripPrefix(path: string, projectPath?: string, worktreePath?: string): string {
  for (const prefix of [worktreePath, projectPath]) {
    if (prefix && path.startsWith(prefix)) {
      const stripped = path.slice(prefix.length);
      return stripped.startsWith("/") ? stripped.slice(1) : stripped;
    }
  }
  return path;
}

function getCategoryIcon(category: string) {
  switch (category) {
    case "command":
      return <Terminal className="h-3 w-3" />;
    case "file_write":
      return <PencilLine className="h-3 w-3" />;
    case "file_read":
      return <FileSearch className="h-3 w-3" />;
    case "web":
      return <Globe className="h-3 w-3" />;
    case "agent":
      return <Bot className="h-3 w-3" />;
    case "mcp":
      return <Plug className="h-3 w-3" />;
    case "task":
      return <ListTodo className="h-3 w-3" />;
    default:
      return <Wrench className="h-3 w-3" />;
  }
}

function getToolIcon(name: string, category?: string) {
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
      return category ? getCategoryIcon(category) : <Terminal className="h-3 w-3" />;
  }
}

function formatSummary(
  name: string,
  input: unknown,
  projectPath?: string,
  worktreePath?: string,
): string {
  if (typeof input === "string") return input;
  if (!input || typeof input !== "object") return JSON.stringify(input);

  const obj = input as Record<string, unknown>;
  const strip = (p: string) => stripPrefix(p, projectPath, worktreePath);

  switch (name) {
    case "Read":
      return strip(String(obj.file_path ?? ""));
    case "Write":
      return strip(String(obj.file_path ?? ""));
    case "Edit":
      return strip(String(obj.file_path ?? ""));
    case "Glob":
      return String(obj.pattern ?? "");
    case "Grep":
      return `${obj.pattern ?? ""}${obj.path ? ` in ${strip(String(obj.path))}` : ""}`;
    case "Bash":
      return String(obj.command ?? obj.description ?? "");
    case "Agent":
      return String(obj.description ?? obj.prompt ?? "").slice(0, 120);
    default:
      return JSON.stringify(input).slice(0, 120);
  }
}

function formatDetail(
  name: string,
  input: unknown,
  projectPath?: string,
  worktreePath?: string,
): string | null {
  if (!input || typeof input !== "object") return null;
  const obj = input as Record<string, unknown>;
  const strip = (p: string) => stripPrefix(p, projectPath, worktreePath);

  switch (name) {
    case "Agent":
      return JSON.stringify(input, null, 2);
    case "Bash":
      return obj.command ? String(obj.command) : null;
    case "Edit":
      return [
        obj.file_path && `File: ${strip(String(obj.file_path))}`,
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

export function ToolUseBlock({
  name,
  input,
  category,
  projectPath,
  worktreePath,
}: ToolUseBlockProps) {
  const [expanded, setExpanded] = useState(false);
  const summary = formatSummary(name, input, projectPath, worktreePath);
  const detail = formatDetail(name, input, projectPath, worktreePath);
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
        {getToolIcon(name, category)}
        <span className="font-medium shrink-0">{name}</span>
        <span className="text-muted-foreground/70 truncate min-w-0">{summary}</span>
      </button>
      {expanded &&
        detail &&
        (name === "Bash" ? (
          <div className="border-t max-h-64 overflow-y-auto">
            <SyntaxHighlighter
              style={oneDark}
              language="bash"
              customStyle={{
                margin: 0,
                padding: "0.5rem",
                fontSize: "0.75rem",
                background: "transparent",
              }}
            >
              {detail}
            </SyntaxHighlighter>
          </div>
        ) : (
          <pre className="p-2 overflow-x-auto text-muted-foreground whitespace-pre-wrap border-t max-h-64 overflow-y-auto break-all">
            {detail}
          </pre>
        ))}
    </div>
  );
}
