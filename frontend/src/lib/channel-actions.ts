import type { WsClient } from "~/lib/ws-client";
import { define, LONG } from "~/lib/ws-rpc";

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
  senderType: "session" | "user" | "persona";
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

const createChannelRpc = define<ChannelInfo, { projectId: string; name: string }>("channel.create");
export function createChannel(ws: WsClient, projectId: string, name: string): Promise<ChannelInfo> {
  return createChannelRpc(ws, { projectId, name });
}

const deleteChannelRpc = define<void, { channelId: string }>("channel.delete");
export function deleteChannel(ws: WsClient, channelId: string): Promise<void> {
  return deleteChannelRpc(ws, { channelId });
}

const dissolveChannelRpc = define<void, { channelId: string }>("channel.dissolve");
export function dissolveChannel(ws: WsClient, channelId: string): Promise<void> {
  return dissolveChannelRpc(ws, { channelId });
}

const dissolveKeepRpc = define<void, { channelId: string }>("channel.dissolve-keep");
export function dissolveChannelKeepHistory(ws: WsClient, channelId: string): Promise<void> {
  return dissolveKeepRpc(ws, { channelId });
}

const joinChannelRpc = define<ChannelInfo, { sessionId: string; channelId: string; role: string }>(
  "channel.join",
);
export function joinChannel(
  ws: WsClient,
  sessionId: string,
  channelId: string,
  role: string,
): Promise<ChannelInfo> {
  return joinChannelRpc(ws, { sessionId, channelId, role });
}

const leaveChannelRpc = define<void, { sessionId: string; channelId: string }>("channel.leave");
export function leaveChannel(ws: WsClient, sessionId: string, channelId: string): Promise<void> {
  return leaveChannelRpc(ws, { sessionId, channelId });
}

const listChannelsRpc = define<ChannelInfo[], { projectId: string }>("channel.list");
export function listChannels(ws: WsClient, projectId: string): Promise<ChannelInfo[]> {
  return listChannelsRpc(ws, { projectId });
}

const channelInfoRpc = define<ChannelInfo, { channelId: string }>("channel.info");
export function getChannelInfo(ws: WsClient, channelId: string): Promise<ChannelInfo> {
  return channelInfoRpc(ws, { channelId });
}

const channelTimelineRpc = define<ChannelMessage[], { channelId: string }>("channel.timeline");
export function getChannelTimeline(ws: WsClient, channelId: string): Promise<ChannelMessage[]> {
  return channelTimelineRpc(ws, { channelId });
}

const sendMessageRpc = define<
  void,
  { senderSessionId: string; targetSessionId: string; content: string }
>("channel.send-message");
export function sendChannelMessage(
  ws: WsClient,
  senderSessionId: string,
  targetSessionId: string,
  content: string,
): Promise<void> {
  return sendMessageRpc(ws, { senderSessionId, targetSessionId, content });
}

const broadcastRpc = define<void, { channelId: string; content: string }>("channel.broadcast");
export function broadcastToChannel(
  ws: WsClient,
  channelId: string,
  content: string,
): Promise<void> {
  return broadcastRpc(ws, { channelId, content });
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

const createSwarmRpc = define<
  CreateSwarmResult,
  { projectId: string; channelName: string; leadSessionId?: string; members: SwarmMemberSpec[] }
>("channel.create-swarm", LONG);
export function createSwarm(
  ws: WsClient,
  projectId: string,
  channelName: string,
  members: SwarmMemberSpec[],
  leadSessionId?: string,
): Promise<CreateSwarmResult> {
  return createSwarmRpc(ws, { projectId, channelName, leadSessionId, members });
}
