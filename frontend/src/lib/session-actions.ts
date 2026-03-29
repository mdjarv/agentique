import type {
  BehaviorPresets,
  CommitMessageResult,
  CreateSessionResult,
  DiffResult,
  DiffStat,
  GitSnapshot,
  PRDescriptionResult,
  SessionCommitResult,
  SessionDeleteBulkResult,
  SessionDeleteBulkResultItem,
} from "~/lib/generated-types";
import type { WsClient } from "~/lib/ws-client";
import type {
  Attachment,
  AutoApproveMode,
  SessionMetadata,
  SessionState,
} from "~/stores/chat-store";
import { useChatStore } from "~/stores/chat-store";
import { useStreamingStore } from "~/stores/streaming-store";

export type { CommitMessageResult, DiffResult, DiffStat, PRDescriptionResult };
export type CommitResult = SessionCommitResult;
export type BulkDeleteResultItem = SessionDeleteBulkResultItem;
export type BulkDeleteResult = SessionDeleteBulkResult;

export const MODELS = ["haiku", "sonnet", "opus"] as const;
export type ModelId = (typeof MODELS)[number];

export const MODEL_LABELS: Record<ModelId, string> = {
  haiku: "Haiku 4.5",
  sonnet: "Sonnet 4.6",
  opus: "Opus 4.6",
};

export interface CreateSessionOpts {
  branch?: string;
  model?: string;
  planMode?: boolean;
  autoApproveMode?: string;
  effort?: string;
  maxBudget?: number;
  maxTurns?: number;
  behaviorPresets?: BehaviorPresets;
}

export async function createSession(
  ws: WsClient,
  projectId: string,
  name: string,
  worktree: boolean,
  opts?: CreateSessionOpts,
): Promise<string> {
  const result = await ws.request<CreateSessionResult>(
    "session.create",
    {
      projectId,
      name,
      worktree,
      branch: opts?.branch,
      model: opts?.model,
      planMode: opts?.planMode,
      autoApproveMode: opts?.autoApproveMode,
      effort: opts?.effort,
      maxBudget: opts?.maxBudget,
      maxTurns: opts?.maxTurns,
      behaviorPresets: opts?.behaviorPresets,
    },
    120000,
  );
  const meta: SessionMetadata = {
    id: result.sessionId,
    projectId,
    name: result.name,
    state: result.state as SessionMetadata["state"],
    connected: result.connected,
    model: result.model as ModelId,
    permissionMode: result.permissionMode,
    autoApproveMode: result.autoApproveMode,
    effort: result.effort,
    maxBudget: result.maxBudget,
    maxTurns: result.maxTurns,
    worktreePath: result.worktreePath,
    worktreeBranch: result.worktreeBranch,
    behaviorPresets: result.behaviorPresets,
    totalCost: 0,
    turnCount: 0,
    commitsAhead: 0,
    commitsBehind: 0,
    gitVersion: 0,
    updatedAt: result.createdAt,
    createdAt: result.createdAt,
  };
  useChatStore.getState().addSession(meta);
  return result.sessionId;
}

export async function renameSession(ws: WsClient, sessionId: string, name: string): Promise<void> {
  await ws.request("session.rename", { sessionId, name });
  useChatStore.getState().setSessionName(sessionId, name);
}

export async function submitQuery(
  ws: WsClient,
  sessionId: string,
  prompt: string,
  attachments?: Attachment[],
): Promise<void> {
  useStreamingStore.getState().clearText(sessionId);
  useChatStore.getState().submitQuery(sessionId, prompt, attachments);

  const payload: Record<string, unknown> = { sessionId, prompt };
  if (attachments && attachments.length > 0) {
    payload.attachments = attachments.map((a) => ({
      name: a.name,
      mimeType: a.mimeType,
      dataUrl: a.dataUrl,
    }));
  }
  await ws.request("session.query", payload);
}

/** Send a message: executes immediately if idle, queues on the backend if running. */
export async function enqueueMessage(
  ws: WsClient,
  sessionId: string,
  prompt: string,
  attachments?: Attachment[],
): Promise<void> {
  const payload: Record<string, unknown> = { sessionId, prompt };
  if (attachments && attachments.length > 0) {
    payload.attachments = attachments.map((a) => ({
      name: a.name,
      mimeType: a.mimeType,
      dataUrl: a.dataUrl,
    }));
  }
  await ws.request("session.enqueue", payload);
}

export async function setSessionModel(
  ws: WsClient,
  sessionId: string,
  model: ModelId,
): Promise<void> {
  await ws.request("session.set-model", { sessionId, model });
  useChatStore.getState().setSessionModel(sessionId, model);
}

export async function getSessionDiff(ws: WsClient, sessionId: string): Promise<DiffResult> {
  return ws.request<DiffResult>("session.diff", { sessionId });
}

export async function setPermissionMode(
  ws: WsClient,
  sessionId: string,
  mode: string,
): Promise<void> {
  await ws.request("session.set-permission", { sessionId, mode });
}

export async function resolveApproval(
  ws: WsClient,
  sessionId: string,
  approvalId: string,
  allow: boolean,
  message?: string,
): Promise<void> {
  await ws.request("session.resolve-approval", {
    sessionId,
    approvalId,
    allow,
    message: message ?? "",
  });
  useChatStore.getState().clearPendingApproval(sessionId);
}

export async function setAutoApproveMode(
  ws: WsClient,
  sessionId: string,
  mode: AutoApproveMode,
): Promise<void> {
  await ws.request("session.set-auto-approve", { sessionId, mode });
  useChatStore.getState().setSessionAutoApproveMode(sessionId, mode);
}

export async function resolveQuestion(
  ws: WsClient,
  sessionId: string,
  questionId: string,
  answers: Record<string, string>,
): Promise<void> {
  await ws.request("session.resolve-question", {
    sessionId,
    questionId,
    answers,
  });
  useChatStore.getState().clearPendingQuestion(sessionId);
}

export async function interruptSession(ws: WsClient, sessionId: string): Promise<void> {
  await ws.request("session.interrupt", { sessionId });
}

export type MergeResult =
  | { status: "merged"; commitHash: string }
  | { status: "conflict"; conflictFiles: string[] }
  | { status: "error"; error: string };

export type CreatePRResult =
  | { status: "created"; url: string }
  | { status: "existing"; url: string }
  | { status: "error"; error: string };

export type MergeMode = "merge" | "complete" | "delete";

export async function mergeSession(
  ws: WsClient,
  sessionId: string,
  mode: MergeMode,
): Promise<MergeResult> {
  return ws.request<MergeResult>("session.merge", { sessionId, mode });
}

export async function createPR(
  ws: WsClient,
  sessionId: string,
  title?: string,
  body?: string,
): Promise<CreatePRResult> {
  return ws.request<CreatePRResult>("session.create-pr", {
    sessionId,
    title,
    body,
  });
}

export async function commitSession(
  ws: WsClient,
  sessionId: string,
  message: string,
): Promise<SessionCommitResult> {
  return ws.request<SessionCommitResult>("session.commit", { sessionId, message });
}

export type RebaseResult =
  | { status: "rebased" }
  | { status: "conflict"; conflictFiles: string[] }
  | { status: "error"; error: string };

export async function rebaseSession(ws: WsClient, sessionId: string): Promise<RebaseResult> {
  return ws.request<RebaseResult>("session.rebase", { sessionId });
}

export async function generateCommitMessage(
  ws: WsClient,
  sessionId: string,
): Promise<CommitMessageResult> {
  return ws.request<CommitMessageResult>("session.generate-commit-message", { sessionId }, 60000);
}

export async function generatePRDescription(
  ws: WsClient,
  sessionId: string,
): Promise<PRDescriptionResult> {
  return ws.request<PRDescriptionResult>("session.generate-pr-description", { sessionId }, 60000);
}

export async function markSessionDone(ws: WsClient, sessionId: string): Promise<void> {
  await ws.request("session.mark-done", { sessionId });
}

export type CleanResult = { status: "cleaned" } | { status: "error"; error: string };

export async function cleanSession(ws: WsClient, sessionId: string): Promise<CleanResult> {
  return ws.request<CleanResult>("session.clean", { sessionId });
}

export interface FileStatus {
  path: string;
  status: "modified" | "added" | "deleted" | "renamed" | "untracked";
}

export interface UncommittedFilesResult {
  files: FileStatus[];
}

export async function getUncommittedFiles(
  ws: WsClient,
  sessionId: string,
): Promise<UncommittedFilesResult> {
  return ws.request<UncommittedFilesResult>("session.uncommitted-files", { sessionId });
}

export async function getUncommittedDiff(ws: WsClient, sessionId: string): Promise<DiffResult> {
  return ws.request<DiffResult>("session.uncommitted-diff", { sessionId });
}

/** Returns true if the session's git snapshot was refreshed within maxAgeMs. */
export function isGitFresh(sessionId: string, maxAgeMs = 10_000): boolean {
  const at = useChatStore.getState().sessions[sessionId]?.meta.gitRefreshedAt;
  return at != null && Date.now() - at < maxAgeMs;
}

/** Refresh git status and apply the response directly to the store (push-independent). */
export async function refreshGitStatus(ws: WsClient, sessionId: string): Promise<void> {
  const gs = await ws.request<GitSnapshot>("session.refresh-git", { sessionId });
  useChatStore.getState().setSessionState(sessionId, gs.state as SessionState, {
    connected: gs.connected,
    hasDirtyWorktree: gs.hasDirtyWorktree,
    hasUncommitted: gs.hasUncommitted,
    worktreeMerged: gs.worktreeMerged,
    completedAt: gs.completedAt,
    commitsAhead: gs.commitsAhead,
    commitsBehind: gs.commitsBehind,
    branchMissing: gs.branchMissing,
    mergeStatus: gs.mergeStatus as SessionMetadata["mergeStatus"],
    mergeConflictFiles: gs.mergeConflictFiles,
    gitOperation: gs.gitOperation ?? "",
    gitVersion: gs.version,
  });
}

export async function resumeSession(ws: WsClient, sessionId: string): Promise<void> {
  const info = await ws.request<{
    state: string;
    connected: boolean;
    hasDirtyWorktree: boolean;
    hasUncommitted: boolean;
    worktreeMerged: boolean;
    completedAt?: string;
    commitsAhead: number;
    commitsBehind: number;
    branchMissing: boolean;
    mergeStatus?: "clean" | "conflicts" | "unknown";
    mergeConflictFiles?: string[];
    gitOperation?: string;
    gitVersion: number;
  }>("session.resume", { sessionId }, 30000);
  useChatStore.getState().setSessionState(sessionId, info.state as SessionState, {
    connected: info.connected,
    hasDirtyWorktree: info.hasDirtyWorktree,
    hasUncommitted: info.hasUncommitted,
    worktreeMerged: info.worktreeMerged,
    completedAt: info.completedAt,
    commitsAhead: info.commitsAhead,
    commitsBehind: info.commitsBehind,
    branchMissing: info.branchMissing,
    mergeStatus: info.mergeStatus,
    mergeConflictFiles: info.mergeConflictFiles,
    gitOperation: info.gitOperation ?? "",
    gitVersion: info.gitVersion,
  });
}

export async function deleteSession(ws: WsClient, sessionId: string): Promise<void> {
  await ws.request("session.delete", { sessionId });
}

export async function deleteSessionsBulk(
  ws: WsClient,
  sessionIds: string[],
): Promise<SessionDeleteBulkResult> {
  return ws.request<SessionDeleteBulkResult>("session.delete-bulk", { sessionIds });
}

export async function stopSession(ws: WsClient, sessionId: string): Promise<void> {
  try {
    await ws.request("session.stop", { sessionId });
  } catch (err) {
    console.error("Failed to stop session:", err);
  }
  useChatStore.getState().removeSession(sessionId);
}
