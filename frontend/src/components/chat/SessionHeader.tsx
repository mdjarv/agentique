import { FolderOpen, GitBranch, GitCompare, Loader2 } from "lucide-react";
import { useState } from "react";
import { DiffView } from "~/components/chat/DiffView";
import { SessionStatusDot } from "~/components/layout/SessionStatusDot";
import { Button } from "~/components/ui/button";
import { useWebSocket } from "~/hooks/useWebSocket";
import { type DiffResult, getSessionDiff } from "~/lib/session-actions";
import type { SessionData } from "~/stores/chat-store";

interface SessionHeaderProps {
	session: SessionData;
}

export function SessionHeader({ session }: SessionHeaderProps) {
	const { meta } = session;
	const ws = useWebSocket();
	const [diffResult, setDiffResult] = useState<DiffResult | null>(null);
	const [diffOpen, setDiffOpen] = useState(false);
	const [loading, setLoading] = useState(false);

	const hasWorktree = !!meta.worktreeBranch;

	async function handleToggleDiff() {
		if (diffOpen) {
			setDiffOpen(false);
			return;
		}
		setLoading(true);
		try {
			const result = await getSessionDiff(ws, meta.id);
			setDiffResult(result);
			setDiffOpen(true);
		} catch (err) {
			console.error("Failed to load diff:", err);
		} finally {
			setLoading(false);
		}
	}

	return (
		<div className="shrink-0">
			<div className="border-b px-4 py-2 flex items-center gap-3 text-sm">
				<SessionStatusDot
					state={meta.state}
					hasUnseenCompletion={session.hasUnseenCompletion}
				/>
				<span className="font-medium truncate">{meta.name}</span>
				{hasWorktree ? (
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
				{hasWorktree && (
					<Button
						variant="ghost"
						size="xs"
						onClick={handleToggleDiff}
						disabled={loading}
						className="shrink-0"
					>
						{loading ? (
							<Loader2 className="h-3 w-3 animate-spin" />
						) : (
							<GitCompare className="h-3 w-3" />
						)}
						Changes
					</Button>
				)}
				<span className="ml-auto text-xs text-muted-foreground shrink-0 capitalize">
					{meta.state}
				</span>
			</div>
			{diffOpen && diffResult && <DiffView result={diffResult} />}
		</div>
	);
}
