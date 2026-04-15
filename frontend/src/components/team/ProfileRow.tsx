import { Trash2 } from "lucide-react";
import { useCallback } from "react";
import { toast } from "sonner";
import { useProfileNavigation } from "~/hooks/useProfileNavigation";
import { useWebSocket } from "~/hooks/useWebSocket";
import type { AgentProfileInfo } from "~/lib/team-actions";
import { deleteAgentProfile } from "~/lib/team-actions";
import { getErrorMessage } from "~/lib/utils";
import { LaunchResumeButton } from "./LaunchResumeButton";
import { ProfileEditorDialog } from "./ProfileEditorDialog";
import { AgentStatusDot, useProfileActiveSession } from "./team-utils";

export function ProfileRow({ profile }: { profile: AgentProfileInfo }) {
  const ws = useWebSocket();
  const activeSession = useProfileActiveSession(profile.id);
  const { project, launching, handleLaunch, handleResume } = useProfileNavigation(
    profile,
    activeSession,
  );

  const handleDelete = useCallback(async () => {
    try {
      await deleteAgentProfile(ws, profile.id);
      toast.success("Profile deleted");
    } catch (e) {
      toast.error(getErrorMessage(e, "Operation failed"));
    }
  }, [ws, profile.id]);

  return (
    <div className="flex items-center justify-between text-xs group hover:bg-muted/30 rounded px-2 py-1.5">
      <div className="flex items-center gap-2 min-w-0">
        <AgentStatusDot profileId={profile.id} />
        {profile.avatar && <span className="text-sm">{profile.avatar}</span>}
        <div className="min-w-0">
          <ProfileEditorDialog profile={profile} />
          <div className="text-muted-foreground truncate">
            {profile.role}
            {project && <span className="ml-1">· {project.name}</span>}
          </div>
        </div>
      </div>
      <div className="flex items-center gap-0.5 opacity-0 group-hover:opacity-100 transition-opacity">
        <LaunchResumeButton
          hasProject={!!profile.projectId}
          activeSession={activeSession}
          launching={launching}
          onLaunch={handleLaunch}
          onResume={handleResume}
        />
        <button
          type="button"
          className="text-muted-foreground hover:text-destructive"
          onClick={handleDelete}
        >
          <Trash2 className="size-3" />
        </button>
      </div>
    </div>
  );
}
