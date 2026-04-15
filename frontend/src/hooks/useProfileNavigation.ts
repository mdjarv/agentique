import { useNavigate } from "@tanstack/react-router";
import { useCallback, useState } from "react";
import { toast } from "sonner";
import type { ActiveSessionRef } from "~/components/team/team-utils";
import { useWebSocket } from "~/hooks/useWebSocket";
import { navigateToSession } from "~/lib/navigation";
import type { AgentProfileInfo } from "~/lib/team-actions";
import { launchAgentProfile } from "~/lib/team-actions";
import { getErrorMessage } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";

export function useProfileNavigation(
  profile: AgentProfileInfo,
  activeSession: ActiveSessionRef | null,
) {
  const ws = useWebSocket();
  const navigate = useNavigate();
  const project = useAppStore((s) => s.projects.find((p) => p.id === profile.projectId));
  const [launching, setLaunching] = useState(false);

  const handleLaunch = useCallback(async () => {
    if (!profile.projectId || launching) return;
    setLaunching(true);
    try {
      const sessionId = await launchAgentProfile(ws, profile);
      navigateToSession(navigate, project?.slug ?? profile.projectId, sessionId);
    } catch (e) {
      toast.error(getErrorMessage(e, "Failed to launch agent"));
    } finally {
      setLaunching(false);
    }
  }, [ws, profile, project, navigate, launching]);

  const handleResume = useCallback(() => {
    if (!activeSession || !project) return;
    navigateToSession(navigate, project.slug, activeSession.sessionId);
  }, [activeSession, project, navigate]);

  return { project, launching, handleLaunch, handleResume };
}
