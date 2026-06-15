import { useCallback, useState } from "react";
import { toast } from "sonner";
import {
  type ChannelInfo,
  createChannel,
  joinChannel,
  leaveChannel,
  listChannels,
} from "~/lib/channel-actions";
import { getErrorMessage } from "~/lib/utils";
import type { WsClient } from "~/lib/ws-client";
import { useChannelStore } from "~/stores/channel-store";
import type { SessionMetadata } from "~/stores/chat-store";

/**
 * Channel membership management for a single session: create/join/leave plus
 * the form state the create/join dialogs bind to. Owns the
 * `useChannelStore.addChannel` mutation that previously lived inline in the
 * header JSX so the store write happens in one place.
 *
 * `handleCreateChannel` / `handleJoinChannel` resolve to `true` on success so
 * the caller can close the dialog; the hook clears its own form state.
 */
export function useChannelManagement(ws: WsClient, meta: SessionMetadata) {
  const sessionId = meta.id;
  const projectId = meta.projectId;
  const hasChannel = !!(meta.channelIds && meta.channelIds.length > 0);

  const [channelName, setChannelName] = useState("");
  const [channelRole, setChannelRole] = useState("");
  const [availableChannels, setAvailableChannels] = useState<ChannelInfo[]>([]);
  const [selectedChannelId, setSelectedChannelId] = useState("");

  /** Reset the create-channel form before opening its dialog. */
  const resetChannelForm = useCallback(() => {
    setChannelName("");
    setChannelRole("");
  }, []);

  const openJoinChannel = useCallback(async (): Promise<boolean> => {
    try {
      const channels = await listChannels(ws, projectId);
      setAvailableChannels(channels);
      setSelectedChannelId(channels[0]?.id ?? "");
      setChannelRole("");
      return true;
    } catch (err) {
      toast.error(getErrorMessage(err, "Failed to load channels"));
      return false;
    }
  }, [ws, projectId]);

  const handleCreateChannel = useCallback(async (): Promise<boolean> => {
    const name = channelName.trim();
    if (!name) return false;
    try {
      const created = await createChannel(ws, projectId, name);
      const ch = await joinChannel(ws, sessionId, created.id, channelRole.trim());
      useChannelStore.getState().addChannel(ch);
      setChannelName("");
      setChannelRole("");
      toast.success("Channel created");
      return true;
    } catch (err) {
      toast.error(getErrorMessage(err, "Failed to create channel"));
      return false;
    }
  }, [ws, projectId, sessionId, channelName, channelRole]);

  const handleJoinChannel = useCallback(async (): Promise<boolean> => {
    if (!selectedChannelId) return false;
    try {
      const ch = await joinChannel(ws, sessionId, selectedChannelId, channelRole.trim());
      useChannelStore.getState().addChannel(ch);
      setChannelRole("");
      toast.success("Joined channel");
      return true;
    } catch (err) {
      toast.error(getErrorMessage(err, "Failed to join channel"));
      return false;
    }
  }, [ws, sessionId, selectedChannelId, channelRole]);

  const handleLeaveChannel = useCallback(async () => {
    try {
      const channelIds = meta.channelIds ?? [];
      await Promise.all(channelIds.map((chId) => leaveChannel(ws, sessionId, chId)));
      toast.success("Left channel");
    } catch (err) {
      toast.error(getErrorMessage(err, "Failed to leave channel"));
    }
  }, [ws, sessionId, meta.channelIds]);

  return {
    hasChannel,
    channelName,
    setChannelName,
    channelRole,
    setChannelRole,
    availableChannels,
    selectedChannelId,
    setSelectedChannelId,
    resetChannelForm,
    openJoinChannel,
    handleCreateChannel,
    handleJoinChannel,
    handleLeaveChannel,
  };
}
