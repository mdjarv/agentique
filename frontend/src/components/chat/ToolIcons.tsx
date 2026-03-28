import {
  Bot,
  Brain,
  CircleHelp,
  ClipboardList,
  FileSearch,
  FileText,
  Globe,
  ListTodo,
  Pencil,
  PencilLine,
  Plug,
  Search,
  Sparkles,
  Terminal,
  Wrench,
} from "lucide-react";
import { cn } from "~/lib/utils";

type ToolColor = "safe" | "effect" | "info" | "agent";

const colorClass: Record<ToolColor, string> = {
  safe: "text-success/70",
  effect: "text-warning/70",
  info: "text-primary/70",
  agent: "text-agent/70",
};

function toolColor(name: string, category?: string): ToolColor {
  switch (name) {
    case "Read":
    case "Glob":
    case "Grep":
      return "safe";
    case "Write":
    case "Edit":
    case "Bash":
      return "effect";
    case "WebFetch":
    case "WebSearch":
    case "TodoWrite":
    case "TodoRead":
      return "info";
    case "EnterPlanMode":
    case "ExitPlanMode":
    case "Agent":
      return "agent";
  }

  switch (category) {
    case "file_read":
      return "safe";
    case "file_write":
    case "command":
      return "effect";
    case "web":
    case "mcp":
    case "task":
    case "meta":
      return "info";
    case "agent":
    case "plan":
    case "question":
      return "agent";
    default:
      return "info";
  }
}

function toolIconElement(name: string, category?: string) {
  switch (name) {
    case "Read":
      return FileText;
    case "Write":
    case "Edit":
      return Pencil;
    case "Glob":
    case "Grep":
      return Search;
    case "WebFetch":
    case "WebSearch":
      return Globe;
    case "EnterPlanMode":
    case "ExitPlanMode":
      return ClipboardList;
  }

  switch (category) {
    case "command":
      return Terminal;
    case "file_write":
      return PencilLine;
    case "file_read":
      return FileSearch;
    case "web":
      return Globe;
    case "agent":
      return Bot;
    case "mcp":
      return Plug;
    case "task":
      return ListTodo;
    case "plan":
      return ClipboardList;
    case "meta":
      return Sparkles;
    case "question":
      return CircleHelp;
    default:
      return Wrench;
  }
}

export function ToolIcon({
  name,
  category,
  className,
}: {
  name: string;
  category?: string;
  className?: string;
}) {
  const Icon = toolIconElement(name, category);
  const color = colorClass[toolColor(name, category)];
  return <Icon className={cn("h-3 w-3", color, className)} />;
}

export function ThinkingIcon({ className }: { className?: string }) {
  return <Brain className={cn("h-3 w-3", colorClass.agent, className)} />;
}
