import { UserMinus } from "lucide-react";
import { useProfileNavigation } from "~/hooks/useProfileNavigation";
import type { AgentProfileInfo } from "~/lib/team-actions";
import { AskPersonaPopover } from "./AskPersonaPopover";
import { LaunchResumeButton } from "./LaunchResumeButton";
import { AgentStatusDot, useProfileActiveSession } from "./team-utils";

export function TeamMemberRow({
  member,
  teamId,
  onRemove,
}: {
  member: AgentProfileInfo;
  teamId: string;
  onRemove: (profileId: string) => void;
}) {
  const activeSession = useProfileActiveSession(member.id);
  const { launching, handleLaunch, handleResume } = useProfileNavigation(member, activeSession);

  return (
    <div className="flex items-center justify-between text-xs group hover:bg-muted/30 rounded px-1 py-0.5">
      <span className="flex items-center gap-1.5">
        <AgentStatusDot profileId={member.id} />
        <span className="font-medium">{member.name}</span>
        {member.role && <span className="text-muted-foreground">({member.role})</span>}
      </span>
      <div className="flex items-center gap-0.5 opacity-0 group-hover:opacity-100 transition-opacity">
        <LaunchResumeButton
          hasProject={!!member.projectId}
          activeSession={activeSession}
          launching={launching}
          onLaunch={handleLaunch}
          onResume={handleResume}
        />
        <AskPersonaPopover profileId={member.id} teamId={teamId} profileName={member.name} />
        <button
          type="button"
          className="text-muted-foreground hover:text-destructive"
          onClick={() => onRemove(member.id)}
        >
          <UserMinus className="size-3" />
        </button>
      </div>
    </div>
  );
}
