import { createFileRoute } from "@tanstack/react-router";
import { Wrench } from "lucide-react";
import { useState } from "react";
import { CollapsibleGroup } from "~/components/chat/CollapsibleGroup";
import { ToolIcon } from "~/components/chat/ToolIcons";
import { ToolUseBlock } from "~/components/chat/ToolUseBlock";

export const Route = createFileRoute("/dev/tool-groups")({
  component: DevToolGroups,
});

interface ToolDef {
  name: string;
  category: string;
}

const TOOL_CYCLE: ToolDef[] = [
  { name: "Read", category: "file_read" },
  { name: "Edit", category: "file_write" },
  { name: "Bash", category: "command" },
  { name: "Grep", category: "file_read" },
  { name: "Glob", category: "file_read" },
  { name: "Write", category: "file_write" },
  { name: "Agent", category: "agent" },
  { name: "WebSearch", category: "web" },
];

const SHORT_INPUTS: Record<string, unknown> = {
  Read: { file_path: "src/main.tsx" },
  Edit: { file_path: "src/app.tsx", old_string: "foo", new_string: "bar" },
  Bash: { command: "go test ./..." },
  Grep: { pattern: "TODO", path: "src/" },
  Glob: { pattern: "**/*.tsx" },
  Write: { file_path: "src/new-file.ts" },
  Agent: { description: "Explore auth", subagent_type: "Explore" },
  WebSearch: { query: "react hooks" },
};

const LONG_INPUTS: Record<string, unknown> = {
  Read: {
    file_path:
      "/home/user/projects/agentique/frontend/src/components/chat/deeply/nested/path/MessageRenderer.tsx",
  },
  Edit: {
    file_path:
      "/home/user/projects/agentique/frontend/src/components/chat/deeply/nested/path/ActivitySegmentView.tsx",
    old_string: "const longVariableName = computeSomethingExpensive()",
    new_string: "const longVariableName = computeSomethingCheap()",
  },
  Bash: {
    command:
      "cd /home/user/projects/agentique && go test -v -count=1 -run TestSessionManager_CreateSession ./backend/internal/session/...",
  },
  Grep: {
    pattern: "CollapsibleGroup|ExpandableRow|ActivitySegment",
    path: "/home/user/projects/agentique/frontend/src/components/",
  },
  Glob: { pattern: "frontend/src/components/**/*.{tsx,ts,css,module.css}" },
  Write: {
    file_path:
      "/home/user/projects/agentique/frontend/src/components/chat/deeply/nested/NewComponent.tsx",
  },
  Agent: {
    description:
      "Investigate the WebSocket reconnection race condition in session history handler and propose a fix with sync barrier",
    subagent_type: "Explore",
  },
  WebSearch: {
    query: "react 19 concurrent mode useTransition best practices server components",
  },
};

const RESULT_CONTENT = [{ type: "text" as const, text: "ok" }];

const GROUP_SIZES = [1, 2, 3, 4, 8, 16, 32, 64];

function buildTools(count: number, long: boolean) {
  const inputs = long ? LONG_INPUTS : SHORT_INPUTS;
  return Array.from({ length: count }, (_, i) => {
    const def = TOOL_CYCLE[i % TOOL_CYCLE.length] as ToolDef;
    return {
      name: def.name,
      category: def.category,
      input: inputs[def.name],
      id: `tool-${count}-${i}`,
    };
  });
}

type TextTier = "short" | "long" | "overflow";
const TIERS: TextTier[] = ["short", "long", "overflow"];

function activityTitle(count: number, tier: TextTier): string {
  const n = count === 1 ? "tool call" : "tool calls";
  switch (tier) {
    case "long":
      return `${count} ${n}, 3 thoughts, and 2 agent messages`;
    case "overflow":
      return `${count} ${n}, 14 thoughts, 8 agent messages, 3 subagent spawns, and 2 plan submissions`;
    default:
      return `${count} ${n}`;
  }
}

function DevToolGroups() {
  const [tierIdx, setTierIdx] = useState(0);
  const tier = TIERS[tierIdx % TIERS.length] as TextTier;

  return (
    <div className="h-full overflow-y-auto">
      <div className="max-w-3xl mx-auto p-6 space-y-6">
        <div className="flex items-center justify-between">
          <h1 className="text-lg font-semibold">Tool Call Groups (1–64)</h1>
          <button
            type="button"
            onClick={() => setTierIdx(tierIdx + 1)}
            className="px-3 py-1.5 text-xs rounded-md border bg-muted/50 hover:bg-muted transition-colors"
          >
            {tier} — click to cycle
          </button>
        </div>

        {GROUP_SIZES.map((size) => {
          const tools = buildTools(size, tier !== "short");
          const trailingIcons = tools.map((t) => (
            <span key={t.id} className="shrink-0">
              <ToolIcon name={t.name} category={t.category} />
            </span>
          ));

          return (
            <div key={size} className="space-y-1">
              <span className="text-xs text-muted-foreground">{size} tool calls</span>
              <CollapsibleGroup
                title={activityTitle(size, tier)}
                icon={<Wrench className="h-3 w-3" />}
                defaultExpanded={false}
                trailingIcons={
                  <span className="flex flex-row-reverse items-center gap-1.5 overflow-hidden">
                    {[...trailingIcons].reverse()}
                  </span>
                }
              >
                {tools.map((t) => (
                  <ToolUseBlock
                    key={t.id}
                    name={t.name}
                    input={t.input}
                    category={t.category}
                    resultContent={RESULT_CONTENT}
                  />
                ))}
              </CollapsibleGroup>
            </div>
          );
        })}
      </div>
    </div>
  );
}
