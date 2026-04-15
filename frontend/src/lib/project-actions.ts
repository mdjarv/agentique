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

export type { ProjectCommitResult, ProjectUncommittedFilesResult, TrackedFilesResult };

export async function getProjectGitStatus(
  ws: WsClient,
  projectId: string,
): Promise<ProjectGitStatus> {
  return ws.request<ProjectGitStatus>("project.git-status", { projectId }, 10_000);
}

export async function fetchProject(ws: WsClient, projectId: string): Promise<ProjectGitStatus> {
  return ws.request<ProjectGitStatus>("project.fetch", { projectId });
}

export async function pushProject(ws: WsClient, projectId: string): Promise<ProjectGitStatus> {
  return ws.request<ProjectGitStatus>("project.push", { projectId }, 120_000);
}

export async function commitProject(
  ws: WsClient,
  projectId: string,
  message: string,
): Promise<ProjectCommitResult> {
  return ws.request<ProjectCommitResult>("project.commit", { projectId, message }, 120_000);
}

export async function listProjectBranches(
  ws: WsClient,
  projectId: string,
): Promise<BranchListResult> {
  return ws.request<BranchListResult>("project.list-branches", { projectId });
}

export async function checkoutProjectBranch(
  ws: WsClient,
  projectId: string,
  branch: string,
): Promise<ProjectGitStatus> {
  return ws.request<ProjectGitStatus>("project.checkout", { projectId, branch });
}

export async function pullProject(ws: WsClient, projectId: string): Promise<ProjectGitStatus> {
  return ws.request<ProjectGitStatus>("project.pull", { projectId }, 120_000);
}

export async function getProjectUncommittedFiles(
  ws: WsClient,
  projectId: string,
): Promise<ProjectUncommittedFilesResult> {
  return ws.request<ProjectUncommittedFilesResult>("project.uncommitted-files", { projectId });
}

export async function generateProjectCommitMessage(
  ws: WsClient,
  projectId: string,
): Promise<CommitMessageResult> {
  return ws.request<CommitMessageResult>("project.generate-commit-message", { projectId }, 120_000);
}

export async function discardProjectChanges(
  ws: WsClient,
  projectId: string,
): Promise<ProjectGitStatus> {
  return ws.request<ProjectGitStatus>("project.discard", { projectId });
}

export async function getTrackedFiles(
  ws: WsClient,
  projectId: string,
): Promise<TrackedFilesResult> {
  return ws.request<TrackedFilesResult>("project.tracked-files", { projectId });
}

export interface CommandFile {
  name: string;
  source: "project" | "user";
  description?: string;
}

export interface CommandsResult {
  commands: CommandFile[];
}

export async function getCommands(ws: WsClient, projectId: string): Promise<CommandsResult> {
  return ws.request<CommandsResult>("project.commands", { projectId });
}

// --- Favorites ---

export async function setProjectFavorite(
  ws: WsClient,
  projectId: string,
  favorite: boolean,
): Promise<Project> {
  return ws.request<Project>("project.set-favorite", { projectId, favorite });
}

export async function reorderProjects(ws: WsClient, projectIds: string[]): Promise<void> {
  await ws.request("project.reorder", { projectIds });
}
