import type { WsClient } from "~/lib/ws-client";
import type { SessionMetadata } from "~/stores/chat-store";
import { useChatStore } from "~/stores/chat-store";

export const MODELS = ["haiku", "sonnet", "opus"] as const;
export type ModelId = (typeof MODELS)[number];

interface SessionCreateResult {
	sessionId: string;
	name: string;
	state: string;
	model: string;
	worktreePath?: string;
	worktreeBranch?: string;
	createdAt: string;
}

export async function createSession(
	ws: WsClient,
	projectId: string,
	name: string,
	worktree: boolean,
	branch?: string,
	model?: string,
): Promise<string> {
	const result = await ws.request<SessionCreateResult>(
		"session.create",
		{ projectId, name, worktree, branch, model },
		120000,
	);
	const meta: SessionMetadata = {
		id: result.sessionId,
		name: result.name,
		state: result.state as SessionMetadata["state"],
		model: result.model as ModelId,
		worktreePath: result.worktreePath,
		worktreeBranch: result.worktreeBranch,
		createdAt: result.createdAt,
	};
	useChatStore.getState().addSession(meta);
	useChatStore.getState().setActiveSessionId(result.sessionId);
	return result.sessionId;
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

export async function getSessionDiff(
	ws: WsClient,
	sessionId: string,
): Promise<DiffResult> {
	return ws.request<DiffResult>("session.diff", { sessionId });
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

export async function interruptSession(
	ws: WsClient,
	sessionId: string,
): Promise<void> {
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

export async function deleteSession(
	ws: WsClient,
	sessionId: string,
): Promise<void> {
	await ws.request("session.delete", { sessionId });
}

export async function stopSession(
	ws: WsClient,
	sessionId: string,
): Promise<void> {
	try {
		await ws.request("session.stop", { sessionId });
	} catch (err) {
		console.error("Failed to stop session:", err);
	}
	const store = useChatStore.getState();
	if (store.activeSessionId === sessionId) {
		const nextId =
			Object.keys(store.sessions).find((id) => id !== sessionId) ?? null;
		store.setActiveSessionId(nextId);
	}
	store.removeSession(sessionId);
}
