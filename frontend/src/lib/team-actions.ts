import { createSession } from "~/lib/session/actions";
import type { WsClient } from "~/lib/ws-client";

// --- Types ---

export interface AgentProfileConfig {
  model?: string;
  permissionMode?: string;
  autoApproveMode?: string;
  effort?: string;
  behaviorPresets?: {
    autoCommit: boolean;
    suggestParallel: boolean;
    planFirst: boolean;
    terse: boolean;
    customInstructions?: string;
  };
}

export interface AgentProfileInfo {
  id: string;
  name: string;
  role: string;
  description: string;
  projectId: string;
  avatar: string;
  config: AgentProfileConfig;
  createdAt: string;
  updatedAt: string;
}

export interface TeamInfo {
  id: string;
  name: string;
  description: string;
  members: AgentProfileInfo[];
  createdAt: string;
  updatedAt: string;
}

// --- Agent Profile actions ---

export function listAgentProfiles(ws: WsClient): Promise<AgentProfileInfo[]> {
  return ws.request<AgentProfileInfo[]>("agent-profile.list");
}

export function createAgentProfile(
  ws: WsClient,
  params: {
    name: string;
    role: string;
    description: string;
    projectId: string;
    avatar: string;
    config: string;
  },
): Promise<AgentProfileInfo> {
  return ws.request<AgentProfileInfo>("agent-profile.create", params);
}

export function updateAgentProfile(
  ws: WsClient,
  params: {
    id: string;
    name: string;
    role: string;
    description: string;
    projectId: string;
    avatar: string;
    config: string;
  },
): Promise<AgentProfileInfo> {
  return ws.request<AgentProfileInfo>("agent-profile.update", params);
}

export function deleteAgentProfile(ws: WsClient, id: string): Promise<void> {
  return ws.request("agent-profile.delete", { id });
}

// --- Team actions ---

export function listTeams(ws: WsClient): Promise<TeamInfo[]> {
  return ws.request<TeamInfo[]>("team.list");
}

export function createTeam(
  ws: WsClient,
  params: { name: string; description: string },
): Promise<TeamInfo> {
  return ws.request<TeamInfo>("team.create", params);
}

export function updateTeam(
  ws: WsClient,
  params: { id: string; name: string; description: string },
): Promise<TeamInfo> {
  return ws.request<TeamInfo>("team.update", params);
}

export function deleteTeam(ws: WsClient, id: string): Promise<void> {
  return ws.request("team.delete", { id });
}

export function addTeamMember(
  ws: WsClient,
  params: { teamId: string; agentProfileId: string; sortOrder: number },
): Promise<TeamInfo> {
  return ws.request<TeamInfo>("team.add-member", params);
}

export function removeTeamMember(
  ws: WsClient,
  params: { teamId: string; agentProfileId: string },
): Promise<TeamInfo> {
  return ws.request<TeamInfo>("team.remove-member", params);
}

// --- Persona types ---

export interface PersonaQueryResult {
  action: string;
  confidence: number;
  redirectTo: string;
  reason: string;
  response: string;
  responseMs: number;
  interactionId: string;
}

export interface PersonaInteraction {
  id: string;
  profileId: string;
  teamId: string;
  askerType: string;
  askerId: string;
  question: string;
  action: string;
  confidence: number;
  response: string;
  redirectTo: string;
  responseTimeMs: number;
  createdAt: string;
}

// --- Persona actions ---

export function askPersona(
  ws: WsClient,
  params: { profileId: string; teamId: string; question: string },
): Promise<PersonaQueryResult> {
  return ws.request<PersonaQueryResult>("persona.query", params);
}

export function listPersonaInteractions(
  ws: WsClient,
  params: { teamId: string; limit?: number; offset?: number },
): Promise<PersonaInteraction[]> {
  return ws.request<PersonaInteraction[]>("persona.list", params);
}

// --- Launch helpers ---

export function launchAgentProfile(ws: WsClient, profile: AgentProfileInfo): Promise<string> {
  const config = profile.config ?? {};
  return createSession(ws, profile.projectId, profile.name, true, {
    model: config.model,
    autoApproveMode: config.autoApproveMode,
    effort: config.effort,
    behaviorPresets: config.behaviorPresets,
    agentProfileId: profile.id,
  });
}

// --- Profile generation ---

export interface GenerateProfileResult {
  name: string;
  role: string;
  description: string;
  avatar: string;
  config: string;
}

export function generateAgentProfile(
  ws: WsClient,
  params: { projectId: string; brief?: string },
): Promise<GenerateProfileResult> {
  return ws.request<GenerateProfileResult>("agent-profile.generate", params, 60000);
}
