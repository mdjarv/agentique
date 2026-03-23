import {
	ChevronDown,
	ExternalLink,
	FolderOpen,
	GitBranch,
	GitMerge,
	Trash2,
} from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { SessionStatusDot } from "~/components/layout/SessionStatusDot";
import {
	AlertDialog,
	AlertDialogAction,
	AlertDialogCancel,
	AlertDialogContent,
	AlertDialogDescription,
	AlertDialogFooter,
	AlertDialogHeader,
	AlertDialogTitle,
} from "~/components/ui/alert-dialog";
import { Button } from "~/components/ui/button";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuTrigger,
} from "~/components/ui/dropdown-menu";
import { useWebSocket } from "~/hooks/useWebSocket";
import { createPR, deleteSession, mergeSession } from "~/lib/session-actions";
import type { SessionData } from "~/stores/chat-store";
import { useChatStore } from "~/stores/chat-store";

interface SessionHeaderProps {
	session: SessionData;
}

export function SessionHeader({ session }: SessionHeaderProps) {
	const { meta } = session;
	const ws = useWebSocket();
	const [showDeleteDialog, setShowDeleteDialog] = useState(false);
	const [merging, setMerging] = useState(false);
	const [creatingPR, setCreatingPR] = useState(false);

	const isWorktree = !!meta.worktreeBranch;
	const isBusy = meta.state === "running";

	const handleMerge = async (cleanup: boolean) => {
		setMerging(true);
		try {
			const result = await mergeSession(ws, meta.id, cleanup);
			if (result.status === "merged") {
				toast.success(
					`Merged${result.commitHash ? ` (${result.commitHash.slice(0, 7)})` : ""}`,
				);
			} else if (result.status === "conflict") {
				toast.error(`Merge conflict in: ${result.conflictFiles?.join(", ")}`);
			} else {
				toast.error(result.error ?? "Merge failed");
			}
		} catch (err) {
			toast.error(err instanceof Error ? err.message : "Merge failed");
		} finally {
			setMerging(false);
		}
	};

	const handleCreatePR = async () => {
		setCreatingPR(true);
		try {
			const result = await createPR(ws, meta.id);
			if (result.status === "created" || result.status === "existing") {
				const label =
					result.status === "existing" ? "PR already exists" : "PR created";
				toast.success(label, {
					action: result.url
						? {
								label: "Open",
								onClick: () => window.open(result.url, "_blank"),
							}
						: undefined,
				});
			} else {
				toast.error(result.error ?? "PR creation failed");
			}
		} catch (err) {
			toast.error(err instanceof Error ? err.message : "PR creation failed");
		} finally {
			setCreatingPR(false);
		}
	};

	const handleDeleteConfirm = async () => {
		try {
			await deleteSession(ws, meta.id);
			const store = useChatStore.getState();
			if (store.activeSessionId === meta.id) {
				const nextId =
					Object.keys(store.sessions).find((id) => id !== meta.id) ?? null;
				store.setActiveSessionId(nextId);
			}
			store.removeSession(meta.id);
		} catch (err) {
			toast.error(err instanceof Error ? err.message : "Delete failed");
		}
	};

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

			<div className="ml-auto flex items-center gap-1.5 shrink-0">
				{isWorktree && (
					<>
						<DropdownMenu>
							<DropdownMenuTrigger asChild>
								<Button variant="ghost" size="xs" disabled={isBusy || merging}>
									<GitMerge className="h-3 w-3" />
									Merge
									<ChevronDown className="h-3 w-3" />
								</Button>
							</DropdownMenuTrigger>
							<DropdownMenuContent align="end">
								<DropdownMenuItem onClick={() => handleMerge(false)}>
									Merge
								</DropdownMenuItem>
								<DropdownMenuItem onClick={() => handleMerge(true)}>
									Merge & Clean Up
								</DropdownMenuItem>
							</DropdownMenuContent>
						</DropdownMenu>
						<Button
							variant="ghost"
							size="xs"
							disabled={isBusy || creatingPR}
							onClick={handleCreatePR}
						>
							<ExternalLink className="h-3 w-3" />
							PR
						</Button>
					</>
				)}
				<Button
					variant="ghost"
					size="xs"
					className="text-muted-foreground hover:text-destructive"
					onClick={() => setShowDeleteDialog(true)}
				>
					<Trash2 className="h-3 w-3" />
				</Button>
			</div>

			<AlertDialog open={showDeleteDialog} onOpenChange={setShowDeleteDialog}>
				<AlertDialogContent>
					<AlertDialogHeader>
						<AlertDialogTitle>Delete session</AlertDialogTitle>
						<AlertDialogDescription>
							This will remove "{meta.name}" and all its data.
							{isWorktree && " The worktree and branch will also be deleted."}
						</AlertDialogDescription>
					</AlertDialogHeader>
					<AlertDialogFooter>
						<AlertDialogCancel>Cancel</AlertDialogCancel>
						<AlertDialogAction onClick={handleDeleteConfirm}>
							Delete
						</AlertDialogAction>
					</AlertDialogFooter>
				</AlertDialogContent>
			</AlertDialog>
		</div>
	);
}
