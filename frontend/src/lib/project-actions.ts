import type { WsClient } from "~/lib/ws-client";
import type { ProjectGitStatus } from "~/stores/app-store";

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

export interface ProjectCommitResult {
  commitHash: string;
}

export async function commitProject(
  ws: WsClient,
  projectId: string,
  message: string,
): Promise<ProjectCommitResult> {
  return ws.request<ProjectCommitResult>("project.commit", { projectId, message });
}

export interface TrackedFilesResult {
  files: string[];
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
