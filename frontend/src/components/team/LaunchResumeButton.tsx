import { ArrowRight, Play } from "lucide-react";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "~/components/ui/tooltip";
import type { ActiveSessionRef } from "./team-utils";

export function LaunchResumeButton({
  hasProject,
  activeSession,
  launching,
  onLaunch,
  onResume,
}: {
  hasProject: boolean;
  activeSession: ActiveSessionRef | null;
  launching: boolean;
  onLaunch: () => void;
  onResume: () => void;
}) {
  if (activeSession) {
    return (
      <TooltipProvider delayDuration={300}>
        <Tooltip>
          <TooltipTrigger asChild>
            <button
              type="button"
              className="text-muted-foreground hover:text-primary"
              onClick={onResume}
            >
              <ArrowRight className="size-3" />
            </button>
          </TooltipTrigger>
          <TooltipContent side="left">
            <p className="text-xs">Go to session</p>
          </TooltipContent>
        </Tooltip>
      </TooltipProvider>
    );
  }

  if (!hasProject) {
    return (
      <TooltipProvider delayDuration={300}>
        <Tooltip>
          <TooltipTrigger asChild>
            <button type="button" className="text-muted-foreground/30 cursor-not-allowed" disabled>
              <Play className="size-3" />
            </button>
          </TooltipTrigger>
          <TooltipContent side="left">
            <p className="text-xs">Set a home project first</p>
          </TooltipContent>
        </Tooltip>
      </TooltipProvider>
    );
  }

  return (
    <TooltipProvider delayDuration={300}>
      <Tooltip>
        <TooltipTrigger asChild>
          <button
            type="button"
            className="text-muted-foreground hover:text-primary disabled:text-muted-foreground/30"
            onClick={onLaunch}
            disabled={launching}
          >
            <Play className="size-3" />
          </button>
        </TooltipTrigger>
        <TooltipContent side="left">
          <p className="text-xs">{launching ? "Launching..." : "Launch session"}</p>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}
