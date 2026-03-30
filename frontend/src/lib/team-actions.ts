import type { WsClient } from "~/lib/ws-client";

export interface TeamInfo {
  id: string;
  projectId: string;
  name: string;
  members: TeamMember[];
  createdAt: string;
}

export interface TeamMember {
  sessionId: string;
  name: string;
  role: string;
  state: string;
  connected: boolean;
  worktreePath?: string;
}

export interface TimelineEvent {
  direction: "sent" | "received";
  senderSessionId: string;
  senderName: string;
  targetSessionId: string;
  targetName: string;
  content: string;
}

export async function createTeam(ws: WsClient, projectId: string, name: string): Promise<TeamInfo> {
  return ws.request<TeamInfo>("team.create", { projectId, name });
}

export async function deleteTeam(ws: WsClient, teamId: string): Promise<void> {
  await ws.request("team.delete", { teamId });
}

export async function dissolveTeam(ws: WsClient, teamId: string): Promise<void> {
  await ws.request("team.dissolve", { teamId });
}

export async function joinTeam(
  ws: WsClient,
  sessionId: string,
  teamId: string,
  role: string,
): Promise<TeamInfo> {
  return ws.request<TeamInfo>("team.join", { sessionId, teamId, role });
}

export async function leaveTeam(ws: WsClient, sessionId: string): Promise<void> {
  await ws.request("team.leave", { sessionId });
}

export async function listTeams(ws: WsClient, projectId: string): Promise<TeamInfo[]> {
  return ws.request<TeamInfo[]>("team.list", { projectId });
}

export async function getTeamInfo(ws: WsClient, teamId: string): Promise<TeamInfo> {
  return ws.request<TeamInfo>("team.info", { teamId });
}

export async function getTeamTimeline(ws: WsClient, teamId: string): Promise<TimelineEvent[]> {
  return ws.request<TimelineEvent[]>("team.timeline", { teamId });
}

export async function sendTeamMessage(
  ws: WsClient,
  senderSessionId: string,
  targetSessionId: string,
  content: string,
): Promise<void> {
  await ws.request("team.send-message", { senderSessionId, targetSessionId, content });
}

export interface SwarmMemberSpec {
  name: string;
  prompt: string;
  role?: string;
  model?: string;
  planMode?: boolean;
  autoApproveMode?: string;
  effort?: string;
  behaviorPresets?: import("~/lib/generated-types").BehaviorPresets;
}

export interface CreateSwarmResult {
  teamId: string;
  sessionIds: string[];
  errors?: string[];
}

export async function createSwarm(
  ws: WsClient,
  projectId: string,
  teamName: string,
  members: SwarmMemberSpec[],
  leadSessionId?: string,
): Promise<CreateSwarmResult> {
  return ws.request<CreateSwarmResult>("team.create-swarm", {
    projectId,
    teamName,
    leadSessionId,
    members,
  });
}
