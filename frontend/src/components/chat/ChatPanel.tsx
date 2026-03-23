import { useNavigate } from "@tanstack/react-router";
import { useEffect } from "react";
import { ApprovalBanner } from "~/components/chat/ApprovalBanner";
import { MessageComposer } from "~/components/chat/MessageComposer";
import { MessageList } from "~/components/chat/MessageList";
import { SessionHeader } from "~/components/chat/SessionHeader";
import { useChatSession } from "~/hooks/useChatSession";
import { useAppStore } from "~/stores/app-store";
import { useChatStore } from "~/stores/chat-store";

interface ChatPanelProps {
	projectId: string;
	initialSessionId?: string;
}

const resumePlaceholders: Record<string, string> = {
	stopped: "Session stopped — send a message to resume...",
	done: "Session complete — send a message to continue...",
	failed: "Session failed — send a message to retry...",
};

export function ChatPanel({ projectId, initialSessionId }: ChatPanelProps) {
	const { sendQuery, interruptSession, loadHistory } = useChatSession(
		projectId,
		initialSessionId,
	);
	const navigate = useNavigate();
	const project = useAppStore((s) =>
		s.projects.find((p) => p.id === projectId),
	);
	const activeSessionId = useChatStore((s) => s.activeSessionId);
	const activeSession = useChatStore((s) =>
		s.activeSessionId ? s.sessions[s.activeSessionId] : undefined,
	);

	// Load history when switching to a session that hasn't been loaded yet
	useEffect(() => {
		if (!activeSessionId) return;
		const s = useChatStore.getState().sessions[activeSessionId];
		if (s && s.turns.length === 0 && s.meta.state !== "draft") {
			loadHistory(activeSessionId);
		}
	}, [activeSessionId, loadHistory]);

	// Sync active session ID to URL search param
	useEffect(() => {
		const isDraftSession = activeSessionId?.startsWith("draft-");
		const session =
			isDraftSession || !activeSessionId ? undefined : activeSessionId;
		navigate({
			to: "/project/$projectId",
			params: { projectId },
			search: { session },
			replace: true,
		});
	}, [activeSessionId, navigate, projectId]);

	const sessionState = activeSession?.meta.state ?? "disconnected";
	const isDraft = sessionState === "draft";
	const resumePlaceholder = resumePlaceholders[sessionState];
	const worktree = activeSession?.meta.worktree ?? false;

	return (
		<div className="flex flex-col h-full" data-project-id={projectId}>
			{activeSession && activeSession.meta.state !== "draft" && (
				<SessionHeader session={activeSession} />
			)}
			<MessageList
				turns={activeSession?.turns ?? []}
				currentAssistantText={activeSession?.currentAssistantText ?? ""}
				sessionState={sessionState}
				projectPath={project?.path}
				worktreePath={activeSession?.meta.worktreePath}
			/>
			{activeSession?.pendingApproval && activeSessionId && (
				<ApprovalBanner
					sessionId={activeSessionId}
					approval={activeSession.pendingApproval}
					projectPath={project?.path}
					worktreePath={activeSession?.meta.worktreePath}
				/>
			)}
			<MessageComposer
					onSend={sendQuery}
					disabled={sessionState === "running"}
					isRunning={sessionState === "running"}
					onInterrupt={() => {
						if (activeSessionId) interruptSession(activeSessionId);
					}}
					isDraft={isDraft}
					placeholder={resumePlaceholder}
					worktree={worktree}
					onWorktreeChange={(v) => {
						if (activeSession) {
							useChatStore
								.getState()
								.setDraftWorktree(activeSession.meta.id, v);
						}
					}}
			/>
		</div>
	);
}
