import type {
  BranchListResult,
  Project,
  ProjectCommitResult,
  ProjectGitStatus,
  Tag,
  TagListResult,
  TrackedFilesResult,
} from "~/lib/generated-types";
import type { WsClient } from "~/lib/ws-client";

export type { ProjectCommitResult, TrackedFilesResult };

export async function getProjectGitStatus(
  ws: WsClient,
  projectId: string,
): Promise<ProjectGitStatus> {
  return ws.request<ProjectGitStatus>("project.git-status", { projectId });
}

export async function fetchProject(ws: WsClient, projectId: string): Promise<ProjectGitStatus> {
  return ws.request<ProjectGitStatus>("project.fetch", { projectId });
}

export async function pushProject(ws: WsClient, projectId: string): Promise<ProjectGitStatus> {
  return ws.request<ProjectGitStatus>("project.push", { projectId });
}

export async function commitProject(
  ws: WsClient,
  projectId: string,
  message: string,
): Promise<ProjectCommitResult> {
  return ws.request<ProjectCommitResult>("project.commit", { projectId, message });
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
  return ws.request<ProjectGitStatus>("project.pull", { projectId });
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

// --- Tags & Favorites ---

export async function listTags(ws: WsClient): Promise<TagListResult> {
  return ws.request<TagListResult>("tag.list", {});
}

export async function createTag(ws: WsClient, name: string, color: string): Promise<Tag> {
  return ws.request<Tag>("tag.create", { name, color });
}

export async function updateTag(
  ws: WsClient,
  id: string,
  name: string,
  color: string,
): Promise<Tag> {
  return ws.request<Tag>("tag.update", { id, name, color });
}

export async function deleteTag(ws: WsClient, id: string): Promise<void> {
  await ws.request("tag.delete", { id });
}

export async function setProjectFavorite(
  ws: WsClient,
  projectId: string,
  favorite: boolean,
): Promise<Project> {
  return ws.request<Project>("project.set-favorite", { projectId, favorite });
}

export async function setProjectTags(
  ws: WsClient,
  projectId: string,
  tagIds: string[],
): Promise<Tag[]> {
  return ws.request<Tag[]>("project.set-tags", { projectId, tagIds });
}
