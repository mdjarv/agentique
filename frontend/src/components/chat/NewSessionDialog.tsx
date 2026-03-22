import { useState } from "react";
import { Button } from "~/components/ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "~/components/ui/dialog";
import { Input } from "~/components/ui/input";
import { Label } from "~/components/ui/label";
import { useChatStore } from "~/stores/chat-store";

interface NewSessionDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit: (name: string, worktree: boolean, branch?: string) => Promise<void>;
}

export function NewSessionDialog({ open, onOpenChange, onSubmit }: NewSessionDialogProps) {
  const sessionCount = useChatStore((s) => Object.keys(s.sessions).length);
  const defaultName = `Session ${sessionCount + 1}`;

  const [name, setName] = useState("");
  const [worktree, setWorktree] = useState(false);
  const [branch, setBranch] = useState("");
  const [submitting, setSubmitting] = useState(false);

  const shortId = () => crypto.randomUUID().slice(0, 8);

  const handleOpenChange = (isOpen: boolean) => {
    onOpenChange(isOpen);
    if (!isOpen) {
      setName("");
      setWorktree(false);
      setBranch("");
    }
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setSubmitting(true);
    try {
      const sessionName = name.trim() || defaultName;
      const sessionBranch = worktree ? branch.trim() || `session-${shortId()}` : undefined;
      await onSubmit(sessionName, worktree, sessionBranch);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>New Session</DialogTitle>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="session-name">Name</Label>
            <Input
              id="session-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={defaultName}
            />
          </div>
          <div className="flex items-center gap-2">
            <input
              id="session-worktree"
              type="checkbox"
              checked={worktree}
              onChange={(e) => setWorktree(e.target.checked)}
              className="h-4 w-4 rounded border-input"
            />
            <Label htmlFor="session-worktree">Use git worktree</Label>
          </div>
          {worktree && (
            <div className="space-y-2">
              <Label htmlFor="session-branch">Branch name</Label>
              <Input
                id="session-branch"
                value={branch}
                onChange={(e) => setBranch(e.target.value)}
                placeholder={`session-${shortId()}`}
              />
            </div>
          )}
          <DialogFooter>
            <DialogClose asChild>
              <Button type="button" variant="outline">
                Cancel
              </Button>
            </DialogClose>
            <Button type="submit" disabled={submitting}>
              {submitting ? "Creating..." : "Create"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
