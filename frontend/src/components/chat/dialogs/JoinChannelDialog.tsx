import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "~/components/ui/alert-dialog";
import type { ChannelInfo } from "~/lib/channel-actions";

export function JoinChannelDialog({
  open,
  onOpenChange,
  channels,
  selectedChannelId,
  onSelectedChannelIdChange,
  channelRole,
  onChannelRoleChange,
  onSubmit,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  channels: ChannelInfo[];
  selectedChannelId: string;
  onSelectedChannelIdChange: (value: string) => void;
  channelRole: string;
  onChannelRoleChange: (value: string) => void;
  onSubmit: () => void;
}) {
  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Join channel</AlertDialogTitle>
          <AlertDialogDescription>Select a channel to join.</AlertDialogDescription>
        </AlertDialogHeader>
        <div className="space-y-2 py-2">
          {channels.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              No channels yet. Create one from the menu.
            </p>
          ) : (
            <select
              value={selectedChannelId}
              onChange={(e) => onSelectedChannelIdChange(e.target.value)}
              className="w-full text-sm bg-background border rounded px-3 py-1.5"
            >
              {channels.map((c) => (
                <option key={c.id} value={c.id}>
                  {c.name} ({c.members.length} members)
                </option>
              ))}
            </select>
          )}
          <input
            value={channelRole}
            onChange={(e) => onChannelRoleChange(e.target.value)}
            placeholder="Your role (optional)"
            className="w-full text-sm bg-background border rounded px-3 py-1.5 outline-none focus:ring-1 focus:ring-ring"
          />
        </div>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction
            onClick={onSubmit}
            disabled={!selectedChannelId || channels.length === 0}
          >
            Join
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
