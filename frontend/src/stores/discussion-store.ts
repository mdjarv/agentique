import { create } from "zustand";
import type { DiscussionInfo } from "~/lib/generated-types";

interface DiscussionState {
  /** Live discussions keyed by channel ID, fed by the discussion.state push. */
  discussions: Record<string, DiscussionInfo>;
  setDiscussion: (d: DiscussionInfo) => void;
  removeDiscussion: (channelId: string) => void;
}

export const useDiscussionStore = create<DiscussionState>((set) => ({
  discussions: {},
  setDiscussion: (d) => set((s) => ({ discussions: { ...s.discussions, [d.channelId]: d } })),
  removeDiscussion: (channelId) =>
    set((s) => {
      const { [channelId]: _removed, ...rest } = s.discussions;
      return { discussions: rest };
    }),
}));
