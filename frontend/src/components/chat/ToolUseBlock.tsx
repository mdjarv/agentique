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
import { useStreamingStore } from "~/stores/streaming-store";

interface ToolUseBlockProps {
  name: string;
  input: unknown;
  category?: string;
  toolId?: string;
  sessionId?: string;
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

export function getCategoryIcon(category: string) {
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

export function getToolIcon(name: string, category?: string) {
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

export function formatSummary(
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
    case "Agent": {
      const desc = String(obj.description ?? obj.prompt ?? "").slice(0, 120);
      const agentType = obj.subagent_type ? `[${obj.subagent_type}] ` : "";
      return `${agentType}${desc}`;
    }
    case "TodoWrite": {
      const todos = Array.isArray(obj.todos) ? obj.todos : [];
      const done = todos.filter((t: Record<string, unknown>) => t.status === "completed").length;
      return `${done}/${todos.length} completed`;
    }
    default:
      return JSON.stringify(input).slice(0, 120);
  }
}

// --- Detail type system ---
// Instead of returning a flat string, we return a tagged detail so the renderer
// can pick the right display (diff, syntax-highlighted, plain text).

interface EditDetail {
  kind: "edit";
  oldString: string;
  newString: string;
}
interface BashDetail {
  kind: "bash";
  command: string;
}
interface TextDetail {
  kind: "text";
  content: string;
}

type Detail = EditDetail | BashDetail | TextDetail;

function buildDetail(
  name: string,
  input: unknown,
  _projectPath?: string,
  _worktreePath?: string,
): Detail | null {
  if (!input || typeof input !== "object") return null;
  const obj = input as Record<string, unknown>;

  switch (name) {
    // These tools have all useful info in the summary line already
    case "Read":
    case "Write":
    case "Glob":
    case "TodoWrite":
    case "TodoRead":
      return null;

    case "Bash":
      return obj.command ? { kind: "bash", command: String(obj.command) } : null;

    case "Edit":
      if (obj.old_string != null && obj.new_string != null) {
        return {
          kind: "edit",
          oldString: String(obj.old_string),
          newString: String(obj.new_string),
        };
      }
      return null;

    case "Agent":
      return obj.prompt ? { kind: "text", content: String(obj.prompt) } : null;

    case "Grep":
      return { kind: "text", content: JSON.stringify(input, null, 2) };

    default: {
      const json = JSON.stringify(input, null, 2);
      return json.length > 100 ? { kind: "text", content: json } : null;
    }
  }
}

// --- Edit diff renderer ---

function prefixLines(text: string, prefix: string): string {
  return text
    .split("\n")
    .map((line) => `${prefix} ${line}`)
    .join("\n");
}

function EditDiffView({ oldString, newString }: { oldString: string; newString: string }) {
  return (
    <div className="border-t max-h-64 overflow-y-auto font-mono text-xs leading-relaxed">
      <pre className="bg-red-500/15 text-red-300 px-2 py-0.5 whitespace-pre-wrap m-0">
        {prefixLines(oldString, "-")}
      </pre>
      <pre className="bg-green-500/15 text-green-300 px-2 py-0.5 whitespace-pre-wrap m-0">
        {prefixLines(newString, "+")}
      </pre>
    </div>
  );
}

// --- Detail renderer ---

function DetailView({ detail }: { detail: Detail }) {
  switch (detail.kind) {
    case "bash":
      return (
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
            {detail.command}
          </SyntaxHighlighter>
        </div>
      );
    case "edit":
      return <EditDiffView oldString={detail.oldString} newString={detail.newString} />;
    case "text":
      return (
        <pre className="p-2 overflow-x-auto text-foreground/80 whitespace-pre-wrap border-t max-h-64 overflow-y-auto break-all">
          {detail.content}
        </pre>
      );
  }
}

// --- Main component ---

export function ToolUseBlock({
  name,
  input,
  category,
  toolId,
  sessionId,
  projectPath,
  worktreePath,
}: ToolUseBlockProps) {
  const [expanded, setExpanded] = useState(false);
  const streamingInput = useStreamingStore((s) =>
    sessionId && toolId ? s.toolInputs[sessionId]?.[toolId] : undefined,
  );
  const isStreaming = !!streamingInput && !input;
  const summary = isStreaming ? "" : formatSummary(name, input, projectPath, worktreePath);
  const detail = isStreaming ? null : buildDetail(name, input, projectPath, worktreePath);
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
        {isStreaming ? (
          <span className="text-muted-foreground/50 font-mono truncate min-w-0">
            {streamingInput}
          </span>
        ) : (
          <span className="text-muted-foreground/70 truncate min-w-0">{summary}</span>
        )}
      </button>
      {expanded && detail && <DetailView detail={detail} />}
    </div>
  );
}
