import { useCallback, useState } from "react";
import { toast } from "sonner";
import { useWebSocket } from "~/hooks/useWebSocket";
import {
  commitProject,
  discardProjectChanges,
  fetchProject,
  getProjectGitStatus,
  pullProject,
  pushProject,
} from "~/lib/project-actions";
import { getErrorMessage } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";

export function useProjectGitActions(projectId: string) {
  const ws = useWebSocket();
  const [pushing, setPushing] = useState(false);
  const [pulling, setPulling] = useState(false);
  const [fetching, setFetching] = useState(false);
  const [committing, setCommitting] = useState(false);

  const handlePush = useCallback(async () => {
    setPushing(true);
    try {
      const status = await pushProject(ws, projectId);
      useAppStore.getState().setProjectGitStatus(status);
      toast.success("Pushed");
    } catch (err) {
      toast.error(getErrorMessage(err, "Push failed"));
    } finally {
      setPushing(false);
    }
  }, [ws, projectId]);

  const handlePull = useCallback(async () => {
    setPulling(true);
    try {
      const status = await pullProject(ws, projectId);
      useAppStore.getState().setProjectGitStatus(status);
      toast.success("Pulled");
    } catch (err) {
      toast.error(getErrorMessage(err, "Pull failed"));
    } finally {
      setPulling(false);
    }
  }, [ws, projectId]);

  const handleFetch = useCallback(async () => {
    setFetching(true);
    try {
      const status = await fetchProject(ws, projectId);
      useAppStore.getState().setProjectGitStatus(status);
    } catch (err) {
      toast.error(getErrorMessage(err, "Fetch failed"));
    } finally {
      setFetching(false);
    }
  }, [ws, projectId]);

  const handleCommit = useCallback(
    async (message: string) => {
      setCommitting(true);
      try {
        await commitProject(ws, projectId, message);
        const status = await getProjectGitStatus(ws, projectId);
        useAppStore.getState().setProjectGitStatus(status);
        toast.success("Committed");
      } catch (err) {
        toast.error(getErrorMessage(err, "Commit failed"));
        throw err;
      } finally {
        setCommitting(false);
      }
    },
    [ws, projectId],
  );

  const [discarding, setDiscarding] = useState(false);

  const handleDiscard = useCallback(async () => {
    setDiscarding(true);
    try {
      const status = await discardProjectChanges(ws, projectId);
      useAppStore.getState().setProjectGitStatus(status);
      toast.success("Changes discarded");
    } catch (err) {
      toast.error(getErrorMessage(err, "Discard failed"));
    } finally {
      setDiscarding(false);
    }
  }, [ws, projectId]);

  return {
    pushing,
    pulling,
    fetching,
    committing,
    discarding,
    handlePush,
    handlePull,
    handleFetch,
    handleCommit,
    handleDiscard,
  };
}
