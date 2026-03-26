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
