import { Loader2 } from "lucide-react";
import { useEffect, useState } from "react";
import { Button } from "~/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "~/components/ui/dialog";
import { Input } from "~/components/ui/input";
import { Textarea } from "~/components/ui/textarea";

interface CreatePRDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  defaultTitle: string;
  onSubmit: (title: string, body: string) => void;
  loading: boolean;
}

export function CreatePRDialog({
  open,
  onOpenChange,
  defaultTitle,
  onSubmit,
  loading,
}: CreatePRDialogProps) {
  const [title, setTitle] = useState(defaultTitle);
  const [body, setBody] = useState("");

  useEffect(() => {
    if (!open) return;
    setTitle(defaultTitle);
    setBody("");
  }, [open, defaultTitle]);

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!title.trim()) return;
    onSubmit(title.trim(), body.trim());
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <form onSubmit={handleSubmit}>
          <DialogHeader>
            <DialogTitle>Create Pull Request</DialogTitle>
          </DialogHeader>
          <div className="grid gap-3 py-4">
            <Input
              placeholder="PR title"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              autoFocus
            />
            <Textarea
              placeholder="Describe the changes..."
              value={body}
              onChange={(e) => setBody(e.target.value)}
              rows={5}
            />
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={loading || !title.trim()}>
              {loading && <Loader2 className="h-4 w-4 animate-spin" />}
              Create PR
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
