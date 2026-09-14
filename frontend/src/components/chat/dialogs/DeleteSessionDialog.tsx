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

export function DeleteSessionDialog({
  open,
  onOpenChange,
  sessionName,
  inWorktree,
  onDelete,
  deleting,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  sessionName: string;
  /**
   * The session has a linked worktree of its own. Only then does deleting it
   * take a worktree and a branch; a session in the main worktree or a plain
   * folder leaves the files where they are.
   */
  inWorktree: boolean;
  onDelete: () => void;
  deleting: boolean;
}) {
  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Delete session</AlertDialogTitle>
          <AlertDialogDescription>
            Delete &quot;{sessionName || "Untitled"}&quot;?{" "}
            {inWorktree
              ? "This removes the worktree, branch, and all session data."
              : "This removes the session and its history. Files it changed in the project stay where they are."}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction onClick={onDelete} disabled={deleting}>
            Delete
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
