import type {
  BranchListResult,
  CommitMessageResult,
  Project,
  ProjectCommitResult,
  ProjectGitStatus,
  ProjectUncommittedFilesResult,
  TrackedFilesResult,
} from "~/lib/generated-types";
import type { WsClient } from "~/lib/ws-client";
import { define, LONG, QUICK } from "~/lib/ws-rpc";

export type { ProjectCommitResult, ProjectUncommittedFilesResult, TrackedFilesResult };

const gitStatusRpc = define<ProjectGitStatus, { projectId: string }>("project.git-status", QUICK);
export function getProjectGitStatus(ws: WsClient, projectId: string): Promise<ProjectGitStatus> {
  return gitStatusRpc(ws, { projectId });
}

const fetchRpc = define<ProjectGitStatus, { projectId: string }>("project.fetch");
export function fetchProject(ws: WsClient, projectId: string): Promise<ProjectGitStatus> {
  return fetchRpc(ws, { projectId });
}

const pushRpc = define<ProjectGitStatus, { projectId: string }>("project.push", LONG);
export function pushProject(ws: WsClient, projectId: string): Promise<ProjectGitStatus> {
  return pushRpc(ws, { projectId });
}

const commitRpc = define<ProjectCommitResult, { projectId: string; message: string }>(
  "project.commit",
  LONG,
);
export function commitProject(
  ws: WsClient,
  projectId: string,
  message: string,
): Promise<ProjectCommitResult> {
  return commitRpc(ws, { projectId, message });
}

const listBranchesRpc = define<BranchListResult, { projectId: string }>("project.list-branches");
export function listProjectBranches(ws: WsClient, projectId: string): Promise<BranchListResult> {
  return listBranchesRpc(ws, { projectId });
}

const checkoutRpc = define<ProjectGitStatus, { projectId: string; branch: string }>(
  "project.checkout",
);
export function checkoutProjectBranch(
  ws: WsClient,
  projectId: string,
  branch: string,
): Promise<ProjectGitStatus> {
  return checkoutRpc(ws, { projectId, branch });
}

const pullRpc = define<ProjectGitStatus, { projectId: string }>("project.pull", LONG);
export function pullProject(ws: WsClient, projectId: string): Promise<ProjectGitStatus> {
  return pullRpc(ws, { projectId });
}

const uncommittedFilesRpc = define<ProjectUncommittedFilesResult, { projectId: string }>(
  "project.uncommitted-files",
);
export function getProjectUncommittedFiles(
  ws: WsClient,
  projectId: string,
): Promise<ProjectUncommittedFilesResult> {
  return uncommittedFilesRpc(ws, { projectId });
}

const generateCommitMessageRpc = define<CommitMessageResult, { projectId: string }>(
  "project.generate-commit-message",
  LONG,
);
export function generateProjectCommitMessage(
  ws: WsClient,
  projectId: string,
): Promise<CommitMessageResult> {
  return generateCommitMessageRpc(ws, { projectId });
}

const discardRpc = define<ProjectGitStatus, { projectId: string }>("project.discard");
export function discardProjectChanges(ws: WsClient, projectId: string): Promise<ProjectGitStatus> {
  return discardRpc(ws, { projectId });
}

const trackedFilesRpc = define<TrackedFilesResult, { projectId: string }>("project.tracked-files");
export function getTrackedFiles(ws: WsClient, projectId: string): Promise<TrackedFilesResult> {
  return trackedFilesRpc(ws, { projectId });
}

export interface CommandFile {
  name: string;
  source: "project" | "user";
  description?: string;
}

export interface CommandsResult {
  commands: CommandFile[];
}

const commandsRpc = define<CommandsResult, { projectId: string }>("project.commands");
export function getCommands(ws: WsClient, projectId: string): Promise<CommandsResult> {
  return commandsRpc(ws, { projectId });
}

// --- Favorites ---

const setFavoriteRpc = define<Project, { projectId: string; favorite: boolean }>(
  "project.set-favorite",
);
export function setProjectFavorite(
  ws: WsClient,
  projectId: string,
  favorite: boolean,
): Promise<Project> {
  return setFavoriteRpc(ws, { projectId, favorite });
}

const setPinnedRpc = define<Project, { projectId: string; pinned: boolean }>("project.set-pinned");
export function setProjectPinned(
  ws: WsClient,
  projectId: string,
  pinned: boolean,
): Promise<Project> {
  return setPinnedRpc(ws, { projectId, pinned });
}

const reorderRpc = define<void, { projectIds: string[] }>("project.reorder");
export function reorderProjects(ws: WsClient, projectIds: string[]): Promise<void> {
  return reorderRpc(ws, { projectIds });
}
