import { useShallow } from "zustand/shallow";
import { type SessionData, useChatStore } from "~/stores/chat-store";

export interface ProjectActivity {
  runningCount: number;
  attentionCount: number;
  failedCount: number;
  unseenCount: number;
}

const EMPTY: ProjectActivity = {
  runningCount: 0,
  attentionCount: 0,
  failedCount: 0,
  unseenCount: 0,
};

export function useProjectActivity(projectId: string): ProjectActivity {
  return useChatStore(
    useShallow((s) => {
      let running = 0;
      let attention = 0;
      let failed = 0;
      let unseen = 0;

      for (const data of Object.values(s.sessions) as SessionData[]) {
        if (data.meta.projectId !== projectId) continue;
        if (data.meta.state === "running") running++;
        if (data.pendingApproval || data.pendingQuestion) attention++;
        if (data.meta.state === "failed") failed++;
        if (data.hasUnseenCompletion) unseen++;
      }

      if (running === 0 && attention === 0 && failed === 0 && unseen === 0) {
        return EMPTY;
      }

      return {
        runningCount: running,
        attentionCount: attention,
        failedCount: failed,
        unseenCount: unseen,
      };
    }),
  );
}
