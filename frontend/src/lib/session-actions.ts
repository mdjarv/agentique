import type { WsClient } from "~/lib/ws-client";
import type { Attachment, SessionMetadata } from "~/stores/chat-store";
import { useChatStore } from "~/stores/chat-store";
import { useStreamingStore } from "~/stores/streaming-store";

export const MODELS = ["haiku", "sonnet", "opus"] as const;
export type ModelId = (typeof MODELS)[number];

export const MODEL_LABELS: Record<ModelId, string> = {
  haiku: "Haiku 4.5",
  sonnet: "Sonnet 4.6",
  opus: "Opus 4.6",
};

interface SessionCreateResult {
  sessionId: string;
  name: string;
  state: string;
  connected: boolean;
  model: string;
  permissionMode: string;
  autoApprove: boolean;
  worktreePath?: string;
  worktreeBranch?: string;
  createdAt: string;
}

export interface CreateSessionOpts {
  branch?: string;
  model?: string;
  planMode?: boolean;
  autoApprove?: boolean;
}

export async function createSession(
  ws: WsClient,
  projectId: string,
  name: string,
  worktree: boolean,
  opts?: CreateSessionOpts,
): Promise<string> {
  const result = await ws.request<SessionCreateResult>(
    "session.create",
    {
      projectId,
      name,
      worktree,
      branch: opts?.branch,
      model: opts?.model,
      planMode: opts?.planMode,
      autoApprove: opts?.autoApprove,
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
    autoApprove: result.autoApprove,
    worktreePath: result.worktreePath,
    worktreeBranch: result.worktreeBranch,
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

export async function setSessionModel(
  ws: WsClient,
  sessionId: string,
  model: ModelId,
): Promise<void> {
  await ws.request("session.set-model", { sessionId, model });
  useChatStore.getState().setSessionModel(sessionId, model);
}

export interface DiffStat {
  path: string;
  insertions: number;
  deletions: number;
  status: string;
}

export interface DiffResult {
  hasDiff: boolean;
  summary: string;
  files: DiffStat[];
  diff: string;
  truncated: boolean;
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

export async function setAutoApprove(
  ws: WsClient,
  sessionId: string,
  enabled: boolean,
): Promise<void> {
  await ws.request("session.set-auto-approve", { sessionId, enabled });
  useChatStore.getState().setSessionAutoApprove(sessionId, enabled);
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

export interface MergeResult {
  status: string;
  commitHash?: string;
  conflictFiles?: string[];
  error?: string;
}

export interface CreatePRResult {
  status: string;
  url?: string;
  error?: string;
}

export async function mergeSession(
  ws: WsClient,
  sessionId: string,
  cleanup: boolean,
): Promise<MergeResult> {
  return ws.request<MergeResult>("session.merge", { sessionId, cleanup });
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

export interface CommitResult {
  commitHash: string;
}

export async function commitSession(
  ws: WsClient,
  sessionId: string,
  message: string,
): Promise<CommitResult> {
  return ws.request<CommitResult>("session.commit", { sessionId, message });
}

export interface RebaseResult {
  status: string;
  conflictFiles?: string[];
  error?: string;
}

export async function rebaseSession(ws: WsClient, sessionId: string): Promise<RebaseResult> {
  return ws.request<RebaseResult>("session.rebase", { sessionId });
}

export interface PRDescriptionResult {
  title: string;
  body: string;
}

export async function generatePRDescription(
  ws: WsClient,
  sessionId: string,
): Promise<PRDescriptionResult> {
  return ws.request<PRDescriptionResult>("session.generate-pr-description", { sessionId }, 60000);
}

export async function deleteSession(ws: WsClient, sessionId: string): Promise<void> {
  await ws.request("session.delete", { sessionId });
}

export async function stopSession(ws: WsClient, sessionId: string): Promise<void> {
  try {
    await ws.request("session.stop", { sessionId });
  } catch (err) {
    console.error("Failed to stop session:", err);
  }
  useChatStore.getState().removeSession(sessionId);
}
