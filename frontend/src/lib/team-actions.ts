import { createSession } from "~/lib/session/actions";
import type { WsClient } from "~/lib/ws-client";
import { define, MEDIUM } from "~/lib/ws-rpc";

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
  systemPromptAdditions?: string;
  capabilities?: string[];
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

export const listAgentProfiles = define<AgentProfileInfo[]>("agent-profile.list");

export const createAgentProfile = define<
  AgentProfileInfo,
  {
    name: string;
    role: string;
    description: string;
    projectId: string;
    avatar: string;
    config: string;
  }
>("agent-profile.create");

export const updateAgentProfile = define<
  AgentProfileInfo,
  {
    id: string;
    name: string;
    role: string;
    description: string;
    projectId: string;
    avatar: string;
    config: string;
  }
>("agent-profile.update");

const deleteAgentProfileRpc = define<void, { id: string }>("agent-profile.delete");
export function deleteAgentProfile(ws: WsClient, id: string): Promise<void> {
  return deleteAgentProfileRpc(ws, { id });
}

// --- Team actions ---

export const listTeams = define<TeamInfo[]>("team.list");

export const createTeam = define<TeamInfo, { name: string; description: string }>("team.create");

export const updateTeam = define<TeamInfo, { id: string; name: string; description: string }>(
  "team.update",
);

const deleteTeamRpc = define<void, { id: string }>("team.delete");
export function deleteTeam(ws: WsClient, id: string): Promise<void> {
  return deleteTeamRpc(ws, { id });
}

export const addTeamMember = define<
  TeamInfo,
  { teamId: string; agentProfileId: string; sortOrder: number }
>("team.add-member");

export const removeTeamMember = define<TeamInfo, { teamId: string; agentProfileId: string }>(
  "team.remove-member",
);

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

export const askPersona = define<
  PersonaQueryResult,
  { profileId: string; teamId: string; question: string }
>("persona.query");

export const listPersonaInteractions = define<
  PersonaInteraction[],
  { teamId: string; limit?: number; offset?: number }
>("persona.list");

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
  systemPromptAdditions: string;
  customInstructions: string;
  capabilities: string[];
  config: string;
}

export const generateAgentProfile = define<
  GenerateProfileResult,
  {
    projectId: string;
    brief?: string;
    name?: string;
    role?: string;
    description?: string;
    avatar?: string;
    systemPromptAdditions?: string;
    customInstructions?: string;
    capabilities?: string[];
  }
>("agent-profile.generate", MEDIUM);
