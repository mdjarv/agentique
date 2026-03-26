import { useState } from "react";
import { Prism as SyntaxHighlighter } from "react-syntax-highlighter";
import { oneDark } from "react-syntax-highlighter/dist/esm/styles/prism";
import { ExpandableRow } from "~/components/chat/ExpandableRow";
import { Markdown } from "~/components/chat/Markdown";
import { ToolIcon } from "~/components/chat/ToolIcons";
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
    case "EnterPlanMode":
      return "Entering plan mode";
    case "ExitPlanMode":
      return "Plan submitted";
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
interface MarkdownDetail {
  kind: "markdown";
  content: string;
}

type Detail = EditDetail | BashDetail | TextDetail | MarkdownDetail;

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
    case "EnterPlanMode":
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

    case "ExitPlanMode":
      return obj.plan ? { kind: "markdown", content: String(obj.plan) } : null;

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
    case "markdown":
      return (
        <div className="border-t max-h-96 overflow-y-auto px-3 py-2 text-sm">
          <Markdown content={detail.content} />
        </div>
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
      <ExpandableRow
        expanded={expanded}
        onToggle={() => setExpanded(!expanded)}
        expandable={hasDetail}
      >
        <ToolIcon name={name} category={category} />
        <span className="font-medium shrink-0">{name}</span>
        {isStreaming ? (
          <span className="text-muted-foreground/50 font-mono truncate min-w-0">
            {streamingInput}
          </span>
        ) : (
          <span className="text-muted-foreground/70 truncate min-w-0">{summary}</span>
        )}
      </ExpandableRow>
      {expanded && detail && <DetailView detail={detail} />}
    </div>
  );
}
