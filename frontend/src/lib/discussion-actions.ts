import type { DiscussionInfo } from "~/lib/generated-types";
import { define, LONG } from "~/lib/ws-rpc";

export type { DiscussionInfo };

export type DiscussionMode = "round-robin" | "parallel";
export type DiscussionScope = "web-only" | "repo-backed";

/** One participant in a discussion-start request. */
export interface DiscussionPersonaInput {
  agentProfileId: string;
  name: string;
  model: string; // "" → use the profile's default
  effort: string; // "" → use the profile's default
  writeAccess: boolean;
  noNamePrefix: boolean;
}

export interface StartDiscussionInput {
  /** Required for repo-backed; empty/omitted for web-only (project-less). */
  projectId: string;
  groupName: string;
  mode: DiscussionMode;
  scope: DiscussionScope;
  autoCommit: boolean;
  personas: DiscussionPersonaInput[];
  prompt: string;
}

/** Start a discussion group; returns the live DiscussionInfo (keyed by channelId). */
export const startDiscussion = define<DiscussionInfo, StartDiscussionInput>(
  "discussion.start",
  LONG,
);

/** Drive another round in a running discussion. */
export const discussionRound = define<void, { channelId: string; prompt: string }>(
  "discussion.round",
  LONG,
);

/** Stop a running discussion (keeps the transcript). */
export const stopDiscussion = define<void, { channelId: string }>("discussion.stop");
