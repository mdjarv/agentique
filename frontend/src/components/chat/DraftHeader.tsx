import { Check, ChevronDown, FolderOpen, Gauge, GitBranch, Plus } from "lucide-react";
import type { EffortLevel } from "~/components/chat/MessageComposer";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "~/components/ui/dropdown-menu";
import { MODELS, MODEL_LABELS, type ModelId } from "~/lib/session-actions";
import { cn } from "~/lib/utils";

interface DraftHeaderProps {
  worktree: boolean;
  onWorktreeChange: (value: boolean) => void;
  model: ModelId;
  onModelChange: (value: ModelId) => void;
  effort: EffortLevel;
  onEffortChange: (value: EffortLevel) => void;
}

export function DraftHeader({
  worktree,
  onWorktreeChange,
  model,
  onModelChange,
  effort,
  onEffortChange,
}: DraftHeaderProps) {
  return (
    <div className="border-b px-4 py-2 flex items-center gap-2 text-sm shrink-0">
      <Plus className="h-4 w-4 text-muted-foreground" />
      <span className="font-medium text-muted-foreground">New session</span>

      <button
        type="button"
        onClick={() => onWorktreeChange(!worktree)}
        className={cn(
          "flex items-center gap-1 text-xs shrink-0 transition-colors",
          worktree ? "text-primary" : "text-muted-foreground hover:text-foreground",
        )}
      >
        {worktree ? <GitBranch className="h-3 w-3" /> : <FolderOpen className="h-3 w-3" />}
        {worktree ? "Worktree" : "Local"}
      </button>

      <div className="ml-auto flex items-center gap-1.5">
        <button
          type="button"
          onClick={() => {
            const levels: EffortLevel[] = ["", "low", "medium", "high"];
            const idx = levels.indexOf(effort);
            const next = levels[(idx + 1) % levels.length] ?? "";
            onEffortChange(next);
          }}
          className={cn(
            "flex items-center gap-1 text-xs rounded border border-border px-1.5 py-0.5 transition-colors",
            effort
              ? "text-blue-500"
              : "text-muted-foreground hover:bg-accent hover:text-accent-foreground",
          )}
        >
          <Gauge className="h-3 w-3" />
          {effort ? `Effort: ${effort}` : "Effort: auto"}
        </button>

        <DropdownMenu>
          <DropdownMenuTrigger
            className={cn(
              "flex items-center gap-1 text-xs rounded border border-border px-1.5 py-0.5 text-muted-foreground transition-colors",
              "hover:bg-accent hover:text-accent-foreground",
              "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
            )}
          >
            {MODEL_LABELS[model]}
            <ChevronDown className="h-3 w-3" />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            {MODELS.map((m) => (
              <DropdownMenuItem key={m} onClick={() => onModelChange(m)} className="text-xs gap-2">
                <Check className={cn("h-3 w-3", m === model ? "opacity-100" : "opacity-0")} />
                {MODEL_LABELS[m]}
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </div>
  );
}
