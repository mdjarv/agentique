import { useNavigate } from "@tanstack/react-router";
import { ChevronDown, ChevronRight, FolderOpen, Plus, Trash2 } from "lucide-react";
import { useState } from "react";
import { useShallow } from "zustand/shallow";
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
import { useWebSocket } from "~/hooks/useWebSocket";
import { deleteProject } from "~/lib/api";
import { deleteSession, interruptSession, stopSession } from "~/lib/session-actions";
import type { Project } from "~/lib/types";
import { cn } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";
import { type SessionState, useChatStore } from "~/stores/chat-store";
import { SessionRow } from "./SessionRow";

const statePriority: Record<SessionState, number> = {
	running: 0,
	starting: 1,
	idle: 2,
	draft: 3,
	disconnected: 4,
	failed: 5,
	stopped: 6,
	done: 7,
};

interface ProjectTreeItemProps {
	project: Project;
	isActive: boolean;
	onNewSession: () => void;
}

export function ProjectTreeItem({
	project,
	isActive,
	onNewSession,
}: ProjectTreeItemProps) {
	const navigate = useNavigate();
	const removeProject = useAppStore((s) => s.removeProject);
	const ws = useWebSocket();
	const [showDeleteDialog, setShowDeleteDialog] = useState(false);

	const sessionIds = useChatStore(useShallow((s) => Object.keys(s.sessions)));
	const sessions = useChatStore((s) => s.sessions);
	const activeSessionId = useChatStore((s) => s.activeSessionId);
	const setActiveSessionId = useChatStore((s) => s.setActiveSessionId);

	const handleProjectClick = () => {
		navigate({ to: "/project/$projectId", params: { projectId: project.id } });
	};

	const handleDeleteClick = (e: React.MouseEvent) => {
		e.stopPropagation();
		setShowDeleteDialog(true);
	};

	const handleDeleteConfirm = async () => {
		try {
			await deleteProject(project.id);
			removeProject(project.id);
			if (isActive) {
				navigate({ to: "/" });
			}
		} catch (err) {
			console.error("Failed to delete project:", err);
		}
	};

	const handleNewSession = (e: React.MouseEvent) => {
		e.stopPropagation();
		onNewSession();
	};

	const handleStopSession = async (
		e: React.MouseEvent,
		sessionId: string,
		state: string,
	) => {
		e.stopPropagation();
		if (state === "running") {
			await interruptSession(ws, sessionId);
		} else {
			await stopSession(ws, sessionId);
		}
	};

	const handleDeleteSession = async (e: React.MouseEvent, sessionId: string) => {
		e.stopPropagation();
		await deleteSession(ws, sessionId);
	};

	const handleSessionClick = (sessionId: string) => {
		if (!isActive) {
			navigate({
				to: "/project/$projectId",
				params: { projectId: project.id },
				search: { session: sessionId },
			});
		}
		setActiveSessionId(sessionId);
	};

	return (
		<div>
			{/* Project row */}
			{/* biome-ignore lint/a11y/useSemanticElements: div with role=button avoids nested button HTML issues */}
			<div
				role="button"
				tabIndex={0}
				onClick={handleProjectClick}
				onKeyDown={(e) => {
					if (e.key === "Enter" || e.key === " ") {
						e.preventDefault();
						handleProjectClick();
					}
				}}
				className={cn(
					"w-full text-left rounded-md px-2 py-1.5 group hover:bg-accent transition-colors cursor-pointer",
					isActive && "bg-accent",
				)}
			>
				<div className="flex items-center gap-1.5">
					{isActive ? (
						<ChevronDown className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
					) : (
						<ChevronRight className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
					)}
					<FolderOpen className="h-4 w-4 shrink-0" />
					<span className="text-sm font-medium truncate flex-1">
						{project.name}
					</span>
					<button
						type="button"
						aria-label="New session"
						onClick={handleNewSession}
						className="opacity-0 group-hover:opacity-100 p-0.5 rounded hover:bg-primary/20 transition-opacity"
					>
						<Plus className="h-3.5 w-3.5" />
					</button>
					<button
						type="button"
						aria-label="Delete project"
						onClick={handleDeleteClick}
						className="opacity-0 group-hover:opacity-100 p-0.5 rounded hover:bg-destructive hover:text-destructive-foreground transition-opacity"
					>
						<Trash2 className="h-3.5 w-3.5" />
					</button>
				</div>
				<p className="text-xs text-muted-foreground truncate mt-0.5 pl-5">
					{project.path}
				</p>
			</div>

			{/* Sessions (only for active project) */}
			{isActive && sessionIds.length > 0 && (
				<div className="ml-4 mt-0.5 space-y-0.5">
					{[...sessionIds]
						.sort((a, b) => {
							const sa = sessions[a]?.meta;
							const sb = sessions[b]?.meta;
							if (!sa || !sb) return 0;
							const pa = statePriority[sa.state] ?? 99;
							const pb = statePriority[sb.state] ?? 99;
							if (pa !== pb) return pa - pb;
							return (
								new Date(sb.createdAt).getTime() -
								new Date(sa.createdAt).getTime()
							);
						})
						.map((id) => {
							const session = sessions[id]?.meta;
							if (!session) return null;
							return (
								<SessionRow
									key={id}
									name={session.name}
									state={session.state}
									hasUnseenCompletion={sessions[id]?.hasUnseenCompletion}
									isActive={id === activeSessionId}
									onClick={() => handleSessionClick(id)}
									onStop={(e) => handleStopSession(e, id, session.state)}
									onDelete={(e) => handleDeleteSession(e, id)}
								/>
							);
						})}
				</div>
			)}

			<AlertDialog open={showDeleteDialog} onOpenChange={setShowDeleteDialog}>
				<AlertDialogContent>
					<AlertDialogHeader>
						<AlertDialogTitle>Delete project</AlertDialogTitle>
						<AlertDialogDescription>
							This will remove "{project.name}" and all its sessions. This
							cannot be undone.
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
