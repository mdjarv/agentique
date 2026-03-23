import { FolderOpen, GitBranch } from "lucide-react";
import { useCallback } from "react";
import { SessionStatusDot } from "~/components/layout/SessionStatusDot";
import { useWebSocket } from "~/hooks/useWebSocket";
import { MODELS, type ModelId, setSessionModel } from "~/lib/session-actions";
import type { SessionData } from "~/stores/chat-store";

interface SessionHeaderProps {
	session: SessionData;
}

const MODEL_LABELS: Record<ModelId, string> = {
	haiku: "Haiku",
	sonnet: "Sonnet",
	opus: "Opus",
};

export function SessionHeader({ session }: SessionHeaderProps) {
	const { meta } = session;
	const ws = useWebSocket();
	const isRunning = meta.state === "running" || meta.state === "starting";
	const isDraft = meta.state === "draft";

	const handleModelChange = useCallback(
		(e: React.ChangeEvent<HTMLSelectElement>) => {
			const model = e.target.value as ModelId;
			if (isDraft) return;
			setSessionModel(ws, meta.id, model).catch(console.error);
		},
		[ws, meta.id, isDraft],
	);

	return (
		<div className="border-b px-4 py-2 flex items-center gap-3 text-sm shrink-0">
			<SessionStatusDot
				state={meta.state}
				hasUnseenCompletion={session.hasUnseenCompletion}
			/>
			<span className="font-medium truncate">{meta.name}</span>
			{meta.worktreeBranch ? (
				<span className="flex items-center gap-1 text-xs text-muted-foreground shrink-0">
					<GitBranch className="h-3 w-3" />
					{meta.worktreeBranch}
				</span>
			) : (
				<span className="flex items-center gap-1 text-xs text-muted-foreground shrink-0">
					<FolderOpen className="h-3 w-3" />
					Local
				</span>
			)}
			{!isDraft && (
				<select
					value={meta.model ?? "opus"}
					onChange={handleModelChange}
					disabled={isRunning}
					className="ml-auto text-xs bg-transparent border border-border rounded px-1.5 py-0.5 text-muted-foreground disabled:opacity-50 cursor-pointer disabled:cursor-not-allowed"
				>
					{MODELS.map((m) => (
						<option key={m} value={m}>
							{MODEL_LABELS[m]}
						</option>
					))}
				</select>
			)}
			<span
				className={`${isDraft ? "ml-auto" : ""} text-xs text-muted-foreground shrink-0 capitalize`}
			>
				{meta.state}
			</span>
		</div>
	);
}
