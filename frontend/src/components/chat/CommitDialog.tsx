import { Loader2 } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { Button } from "~/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "~/components/ui/dialog";
import { Input } from "~/components/ui/input";

interface CommitDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit: (message: string) => void;
  loading: boolean;
}

export function CommitDialog({ open, onOpenChange, onSubmit, loading }: CommitDialogProps) {
  const [message, setMessage] = useState("");
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (open) {
      setMessage("");
    }
  }, [open]);

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const trimmed = message.trim();
    if (!trimmed || loading) return;
    onSubmit(trimmed);
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Commit changes</DialogTitle>
        </DialogHeader>
        <form onSubmit={handleSubmit}>
          <Input
            ref={inputRef}
            value={message}
            onChange={(e) => setMessage(e.target.value)}
            placeholder="Commit message..."
            autoFocus
            disabled={loading}
          />
          <DialogFooter className="mt-4">
            <Button type="submit" size="sm" disabled={loading || !message.trim()}>
              {loading && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              Commit
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
