import { useEffect } from "react";
import type { useWebSocket } from "~/hooks/useWebSocket";
import type { DiscussionInfo } from "~/lib/generated-types";
import { useDiscussionStore } from "~/stores/discussion-store";

/**
 * Subscribes to discussion lifecycle pushes. `discussion.state` (typed) carries
 * round/running updates; `discussion.stopped` (untyped) removes a finished
 * discussion. Wired centrally in useGlobalSubscriptions.
 */
export function useDiscussionSubscriptions(ws: ReturnType<typeof useWebSocket>) {
  useEffect(() => {
    const unsubState = ws.subscribe("discussion.state", (payload) => {
      useDiscussionStore.getState().setDiscussion(payload as DiscussionInfo);
    });
    const unsubStopped = ws.subscribe("discussion.stopped", (payload) => {
      const { channelId } = payload as { channelId: string };
      useDiscussionStore.getState().removeDiscussion(channelId);
    });
    return () => {
      unsubState();
      unsubStopped();
    };
  }, [ws]);
}
