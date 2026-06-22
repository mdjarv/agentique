import { toWireAttachment } from "~/lib/attachment-utils";
import type {
  BehaviorPresets,
  CommitMessageResult,
  CreateSessionResult,
  DiffResult,
  DiffStat,
  GitSnapshot,
  ListModelsResult,
  PRDescriptionResult,
  SessionCommitResult,
  SessionDeleteBulkResult,
  SessionDeleteBulkResultItem,
} from "~/lib/generated-types";
import type { WsClient } from "~/lib/ws-client";
import { define, LONG, QUICK } from "~/lib/ws-rpc";
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

export const PROVIDERS = ["claude", "codex"] as const;
export type ProviderId = (typeof PROVIDERS)[number];

export const PROVIDER_LABELS: Record<ProviderId, string> = {
  claude: "Claude",
  codex: "Codex",
};

export const MODELS = [
  "haiku",
  "sonnet",
  "opus",
  "fable",
  "sonnet[1m]",
  "opus[1m]",
  "gpt-5",
  "gpt-5-codex",
  "gpt-5-mini",
] as const;
export type ModelId = (typeof MODELS)[number];

export const MODEL_LABELS: Record<ModelId, string> = {
  haiku: "Haiku 4.5",
  sonnet: "Sonnet 4.6",
  opus: "Opus 4.8",
  fable: "Fable 5",
  "sonnet[1m]": "Sonnet 4.6 (1M)",
  "opus[1m]": "Opus 4.8 (1M)",
  "gpt-5": "GPT-5",
  "gpt-5-codex": "GPT-5 Codex",
  "gpt-5-mini": "GPT-5 Mini",
};

export const MODEL_PROVIDER: Record<ModelId, ProviderId> = {
  haiku: "claude",
  sonnet: "claude",
  opus: "claude",
  fable: "claude",
  "sonnet[1m]": "claude",
  "opus[1m]": "claude",
  "gpt-5": "codex",
  "gpt-5-codex": "codex",
  "gpt-5-mini": "codex",
};

export const DEFAULT_MODEL_FOR_PROVIDER: Record<ProviderId, ModelId> = {
  claude: "sonnet",
  codex: "gpt-5",
};

export function providerForModel(model: ModelId | string | undefined): ProviderId {
  if (!model) return "claude";
  return MODEL_PROVIDER[model as ModelId] ?? "claude";
}

export interface CreateSessionOpts {
  branch?: string;
  provider?: ProviderId;
  model?: string;
  planMode?: boolean;
  autoApproveMode?: string;
  effort?: string;
  maxBudget?: number;
  maxTurns?: number;
  behaviorPresets?: BehaviorPresets;
  agentProfileId?: string;
}

export const listProviderModels = define<ListModelsResult>("providers.models", QUICK);

const createSessionRpc = define<CreateSessionResult, Record<string, unknown>>(
  "session.create",
  LONG,
);

export async function createSession(
  ws: WsClient,
  projectId: string,
  name: string,
  worktree: boolean,
  opts?: CreateSessionOpts,
): Promise<string> {
  const result = await createSessionRpc(ws, {
    projectId,
    name,
    worktree,
    branch: opts?.branch,
    provider: opts?.provider,
    model: opts?.model,
    planMode: opts?.planMode,
    autoApproveMode: opts?.autoApproveMode,
    effort: opts?.effort,
    maxBudget: opts?.maxBudget,
    maxTurns: opts?.maxTurns,
    behaviorPresets: opts?.behaviorPresets,
    agentProfileId: opts?.agentProfileId,
  });
  const meta: SessionMetadata = {
    id: result.sessionId,
    projectId,
    name: result.name,
    state: result.state as SessionMetadata["state"],
    connected: result.connected,
    provider: result.provider,
    capabilities: result.capabilities,
    model: result.model as ModelId,
    permissionMode: result.permissionMode,
    autoApproveMode: result.autoApproveMode,
    effort: result.effort,
    maxBudget: result.maxBudget,
    maxTurns: result.maxTurns,
    worktreePath: result.worktreePath,
    worktreeBranch: result.worktreeBranch,
    behaviorPresets: result.behaviorPresets,
    agentProfileId: result.agentProfileId,
    agentProfileName: result.agentProfileName,
    agentProfileAvatar: result.agentProfileAvatar,
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
    payload.attachments = attachments.map(toWireAttachment);
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
  // For non-running sessions, create an optimistic turn for immediate feedback.
  // The session.turn-started handler in useSessionEventSubscription deduplicates
  // with this (peeling any <brain> recall envelope before matching).
  const sessionState = useChatStore.getState().sessions[sessionId]?.meta.state;
  const isOptimistic = sessionState !== "running";
  if (isOptimistic) {
    useStreamingStore.getState().clearText(sessionId);
    useChatStore.getState().submitQuery(sessionId, prompt, attachments);
  }

  const payload: Record<string, unknown> = { sessionId, prompt };
  if (attachments && attachments.length > 0) {
    payload.attachments = attachments.map(toWireAttachment);
  }

  try {
    await ws.request("session.enqueue", payload);
  } catch (err) {
    if (isOptimistic) {
      useChatStore.getState().rollbackOptimisticTurn(sessionId, prompt);
    }
    throw err;
  }
}

export async function setSessionModel(
  ws: WsClient,
  sessionId: string,
  model: ModelId,
): Promise<void> {
  await ws.request("session.set-model", { sessionId, model });
  useChatStore.getState().setSessionModel(sessionId, model);
}

export async function setSessionIcon(
  ws: WsClient,
  sessionId: string,
  icon: string | undefined,
): Promise<void> {
  await ws.request("session.set-icon", { sessionId, icon: icon ?? "" });
  useChatStore.getState().setSessionIcon(sessionId, icon);
}

const sessionDiffRpc = define<DiffResult, { sessionId: string }>("session.diff");
export function getSessionDiff(ws: WsClient, sessionId: string): Promise<DiffResult> {
  return sessionDiffRpc(ws, { sessionId });
}

const setPermissionRpc = define<void, { sessionId: string; mode: string }>(
  "session.set-permission",
);
export function setPermissionMode(ws: WsClient, sessionId: string, mode: string): Promise<void> {
  return setPermissionRpc(ws, { sessionId, mode });
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

/**
 * Resolves the pending AskUserQuestion with a sentinel that tells Claude the
 * user dismissed it. The turn keeps running so Claude can respond to whatever
 * the user types next.
 */
export async function dismissQuestion(
  ws: WsClient,
  sessionId: string,
  questionId: string,
): Promise<void> {
  await ws.request("session.dismiss-question", { sessionId, questionId });
  useChatStore.getState().clearPendingQuestion(sessionId);
}

const interruptRpc = define<void, { sessionId: string }>("session.interrupt");
export function interruptSession(ws: WsClient, sessionId: string): Promise<void> {
  return interruptRpc(ws, { sessionId });
}

export type MergeResult =
  | { status: "merged"; commitHash: string }
  | { status: "needs_rebase" }
  | { status: "conflict"; conflictFiles: string[] }
  | { status: "dirty_worktree"; error: string }
  | { status: "error"; error: string };

export type CreatePRResult =
  | { status: "created"; url: string }
  | { status: "existing"; url: string }
  | { status: "error"; error: string };

export type MergeMode = "merge" | "complete" | "delete";

const mergeRpc = define<MergeResult, { sessionId: string; mode: MergeMode }>("session.merge", LONG);
export function mergeSession(
  ws: WsClient,
  sessionId: string,
  mode: MergeMode,
): Promise<MergeResult> {
  return mergeRpc(ws, { sessionId, mode });
}

const createPRRpc = define<CreatePRResult, { sessionId: string; title?: string; body?: string }>(
  "session.create-pr",
  LONG,
);
export function createPR(
  ws: WsClient,
  sessionId: string,
  title?: string,
  body?: string,
): Promise<CreatePRResult> {
  return createPRRpc(ws, { sessionId, title, body });
}

const commitRpc = define<SessionCommitResult, { sessionId: string; message: string }>(
  "session.commit",
  LONG,
);
export function commitSession(
  ws: WsClient,
  sessionId: string,
  message: string,
): Promise<SessionCommitResult> {
  return commitRpc(ws, { sessionId, message });
}

export type RebaseResult =
  | { status: "rebased" }
  | { status: "conflict"; conflictFiles: string[] }
  | { status: "error"; error: string };

const rebaseRpc = define<RebaseResult, { sessionId: string }>("session.rebase", LONG);
export function rebaseSession(ws: WsClient, sessionId: string): Promise<RebaseResult> {
  return rebaseRpc(ws, { sessionId });
}

const generateCommitMessageRpc = define<CommitMessageResult, { sessionId: string }>(
  "session.generate-commit-message",
  LONG,
);
export function generateCommitMessage(
  ws: WsClient,
  sessionId: string,
): Promise<CommitMessageResult> {
  return generateCommitMessageRpc(ws, { sessionId });
}

const generatePRDescriptionRpc = define<PRDescriptionResult, { sessionId: string }>(
  "session.generate-pr-description",
  LONG,
);
export function generatePRDescription(
  ws: WsClient,
  sessionId: string,
): Promise<PRDescriptionResult> {
  return generatePRDescriptionRpc(ws, { sessionId });
}

const generateNameRpc = define<{ name: string }, { sessionId: string }>(
  "session.generate-name",
  LONG,
);
export function generateSessionName(ws: WsClient, sessionId: string): Promise<{ name: string }> {
  return generateNameRpc(ws, { sessionId });
}

const markDoneRpc = define<void, { sessionId: string }>("session.mark-done");
export function markSessionDone(ws: WsClient, sessionId: string): Promise<void> {
  return markDoneRpc(ws, { sessionId });
}

export type CleanResult = { status: "cleaned" } | { status: "error"; error: string };

const cleanRpc = define<CleanResult, { sessionId: string }>("session.clean", LONG);
export function cleanSession(ws: WsClient, sessionId: string): Promise<CleanResult> {
  return cleanRpc(ws, { sessionId });
}

export interface FileStatus {
  path: string;
  status: "modified" | "added" | "deleted" | "renamed" | "untracked";
}

export interface UncommittedFilesResult {
  files: FileStatus[];
}

const uncommittedFilesRpc = define<UncommittedFilesResult, { sessionId: string }>(
  "session.uncommitted-files",
);
export function getUncommittedFiles(
  ws: WsClient,
  sessionId: string,
): Promise<UncommittedFilesResult> {
  return uncommittedFilesRpc(ws, { sessionId });
}

export interface CommitLogEntry {
  hash: string;
  message: string;
  body?: string;
  timestamp: string;
}

export interface CommitLogResult {
  commits: CommitLogEntry[];
}

const commitLogRpc = define<CommitLogResult, { sessionId: string }>("session.commit-log");
export function getCommitLog(ws: WsClient, sessionId: string): Promise<CommitLogResult> {
  return commitLogRpc(ws, { sessionId });
}

const uncommittedDiffRpc = define<DiffResult, { sessionId: string }>("session.uncommitted-diff");
export function getUncommittedDiff(ws: WsClient, sessionId: string): Promise<DiffResult> {
  return uncommittedDiffRpc(ws, { sessionId });
}

export interface PRStatusResult {
  number: number;
  state: string; // OPEN, MERGED, CLOSED
  isDraft: boolean;
  checksStatus: string; // pass, fail, pending, none
}

const prStatusRpc = define<PRStatusResult, { sessionId: string }>("session.pr-status");
export function getPRStatus(ws: WsClient, sessionId: string): Promise<PRStatusResult> {
  return prStatusRpc(ws, { sessionId });
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

const resumeRpc = define<
  {
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
    worktreeBranch?: string;
    worktreePath?: string;
  },
  { sessionId: string }
>("session.resume", LONG);

export async function resumeSession(ws: WsClient, sessionId: string): Promise<void> {
  const info = await resumeRpc(ws, { sessionId });
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
    worktreeBranch: info.worktreeBranch,
    worktreePath: info.worktreePath,
  });
}

const deleteRpc = define<void, { sessionId: string }>("session.delete");
export function deleteSession(ws: WsClient, sessionId: string): Promise<void> {
  return deleteRpc(ws, { sessionId });
}

const deleteBulkRpc = define<SessionDeleteBulkResult, { sessionIds: string[] }>(
  "session.delete-bulk",
);
export function deleteSessionsBulk(
  ws: WsClient,
  sessionIds: string[],
): Promise<SessionDeleteBulkResult> {
  return deleteBulkRpc(ws, { sessionIds });
}

const stopRpc = define<void, { sessionId: string }>("session.stop");
export function stopSession(ws: WsClient, sessionId: string): Promise<void> {
  return stopRpc(ws, { sessionId });
}

export async function restartSession(ws: WsClient, sessionId: string): Promise<void> {
  await stopSession(ws, sessionId);
  await resumeSession(ws, sessionId);
}

const resetConversationRpc = define<void, { sessionId: string }>("session.reset-conversation");
export function resetConversation(ws: WsClient, sessionId: string): Promise<void> {
  return resetConversationRpc(ws, { sessionId });
}
