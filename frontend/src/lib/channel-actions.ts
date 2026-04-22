import type { WsClient } from "~/lib/ws-client";

export interface ChannelInfo {
  id: string;
  projectId: string;
  name: string;
  members: ChannelMember[];
  createdAt: string;
}

export interface ChannelMember {
  sessionId: string;
  name: string;
  role: string;
  state: string;
  connected: boolean;
  worktreePath?: string;
}

export type AgentMessageType =
  | "plan"
  | "progress"
  | "done"
  | "message"
  | "clarification"
  | "introduction"
  | "spawn";

/** Unified channel message from the messages table. */
export interface ChannelMessage {
  id: string;
  channelId: string;
  senderType: "session" | "user";
  senderId: string;
  senderName: string;
  content: string;
  messageType?: AgentMessageType;
  metadata?: { targetSessionId?: string; targetName?: string; [key: string]: unknown };
  createdAt: string;
}

/**
 * Legacy timeline event shape — kept during transition for backward compat.
 * @deprecated Use ChannelMessage instead.
 */
export interface TimelineEvent {
  direction: "sent" | "received";
  fromUser?: boolean;
  senderSessionId: string;
  senderName: string;
  targetSessionId: string;
  targetName: string;
  content: string;
  messageType?: AgentMessageType;
}

export async function createChannel(
  ws: WsClient,
  projectId: string,
  name: string,
): Promise<ChannelInfo> {
  return ws.request<ChannelInfo>("channel.create", { projectId, name });
}

export async function deleteChannel(ws: WsClient, channelId: string): Promise<void> {
  await ws.request("channel.delete", { channelId });
}

export async function dissolveChannel(ws: WsClient, channelId: string): Promise<void> {
  await ws.request("channel.dissolve", { channelId });
}

export async function dissolveChannelKeepHistory(ws: WsClient, channelId: string): Promise<void> {
  await ws.request("channel.dissolve-keep", { channelId });
}

export async function joinChannel(
  ws: WsClient,
  sessionId: string,
  channelId: string,
  role: string,
): Promise<ChannelInfo> {
  return ws.request<ChannelInfo>("channel.join", { sessionId, channelId, role });
}

export async function leaveChannel(
  ws: WsClient,
  sessionId: string,
  channelId: string,
): Promise<void> {
  await ws.request("channel.leave", { sessionId, channelId });
}

export async function listChannels(ws: WsClient, projectId: string): Promise<ChannelInfo[]> {
  return ws.request<ChannelInfo[]>("channel.list", { projectId });
}

export async function getChannelInfo(ws: WsClient, channelId: string): Promise<ChannelInfo> {
  return ws.request<ChannelInfo>("channel.info", { channelId });
}

export async function getChannelTimeline(
  ws: WsClient,
  channelId: string,
): Promise<ChannelMessage[]> {
  return ws.request<ChannelMessage[]>("channel.timeline", { channelId });
}

export async function sendChannelMessage(
  ws: WsClient,
  senderSessionId: string,
  targetSessionId: string,
  content: string,
): Promise<void> {
  await ws.request("channel.send-message", { senderSessionId, targetSessionId, content });
}

export async function broadcastToChannel(
  ws: WsClient,
  channelId: string,
  content: string,
): Promise<void> {
  await ws.request("channel.broadcast", { channelId, content });
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
  channelId: string;
  sessionIds: string[];
  errors?: string[];
}

export async function createSwarm(
  ws: WsClient,
  projectId: string,
  channelName: string,
  members: SwarmMemberSpec[],
  leadSessionId?: string,
): Promise<CreateSwarmResult> {
  return ws.request<CreateSwarmResult>(
    "channel.create-swarm",
    { projectId, channelName, leadSessionId, members },
    120_000,
  );
}
