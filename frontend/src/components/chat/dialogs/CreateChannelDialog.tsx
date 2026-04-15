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

export function CreateChannelDialog({
  open,
  onOpenChange,
  channelName,
  onChannelNameChange,
  channelRole,
  onChannelRoleChange,
  onSubmit,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  channelName: string;
  onChannelNameChange: (value: string) => void;
  channelRole: string;
  onChannelRoleChange: (value: string) => void;
  onSubmit: () => void;
}) {
  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Create channel</AlertDialogTitle>
          <AlertDialogDescription>
            Create a new channel and add this session as the first member.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <div className="space-y-2 py-2">
          <input
            value={channelName}
            onChange={(e) => onChannelNameChange(e.target.value)}
            placeholder="Channel name"
            className="w-full text-sm bg-background border rounded px-3 py-1.5 outline-none focus:ring-1 focus:ring-ring"
            onKeyDown={(e) => {
              if (e.key === "Enter") onSubmit();
            }}
          />
          <input
            value={channelRole}
            onChange={(e) => onChannelRoleChange(e.target.value)}
            placeholder="Your role (optional)"
            className="w-full text-sm bg-background border rounded px-3 py-1.5 outline-none focus:ring-1 focus:ring-ring"
          />
        </div>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction onClick={onSubmit} disabled={!channelName.trim()}>
            Create
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
