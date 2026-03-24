import { Loader2, Sparkles } from "lucide-react";
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
import { useWebSocket } from "~/hooks/useWebSocket";
import { generatePRDescription } from "~/lib/session-actions";

interface CreatePRDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  sessionId: string;
  defaultTitle: string;
  onSubmit: (title: string, body: string) => void;
  loading: boolean;
}

export function CreatePRDialog({
  open,
  onOpenChange,
  sessionId,
  defaultTitle,
  onSubmit,
  loading,
}: CreatePRDialogProps) {
  const ws = useWebSocket();
  const [title, setTitle] = useState(defaultTitle);
  const [body, setBody] = useState("");
  const [generating, setGenerating] = useState(false);

  useEffect(() => {
    if (!open) return;
    setTitle(defaultTitle);
    setBody("");

    setGenerating(true);
    generatePRDescription(ws, sessionId)
      .then((result) => {
        setTitle(result.title);
        setBody(result.body);
      })
      .catch(() => {
        // Keep defaults on failure
      })
      .finally(() => setGenerating(false));
  }, [open, sessionId, ws, defaultTitle]);

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
            <div className="relative">
              <Input
                placeholder="PR title"
                value={title}
                onChange={(e) => setTitle(e.target.value)}
                autoFocus
                disabled={generating}
              />
              {generating && (
                <div className="absolute right-2 top-1/2 -translate-y-1/2">
                  <Sparkles className="h-4 w-4 animate-pulse text-muted-foreground" />
                </div>
              )}
            </div>
            <div className="relative">
              <Textarea
                placeholder="Describe the changes..."
                value={body}
                onChange={(e) => setBody(e.target.value)}
                rows={5}
                disabled={generating}
              />
              {generating && (
                <div className="absolute right-2 top-2">
                  <Sparkles className="h-4 w-4 animate-pulse text-muted-foreground" />
                </div>
              )}
            </div>
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={loading || generating || !title.trim()}>
              {loading && <Loader2 className="h-4 w-4 animate-spin" />}
              Create PR
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
