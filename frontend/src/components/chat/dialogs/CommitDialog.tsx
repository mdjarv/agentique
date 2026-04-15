import { Loader2, Sparkles } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";
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
import { generateCommitMessage } from "~/lib/session/actions";

interface CommitDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  sessionId: string;
  defaultTitle?: string;
  onSubmit: (message: string) => void;
  loading: boolean;
}

export function CommitDialog({
  open,
  onOpenChange,
  sessionId,
  defaultTitle,
  onSubmit,
  loading,
}: CommitDialogProps) {
  const ws = useWebSocket();
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [generating, setGenerating] = useState(false);

  useEffect(() => {
    if (open) {
      setTitle(defaultTitle ?? "");
      setDescription("");
    }
  }, [open, defaultTitle]);

  const handleGenerate = () => {
    setGenerating(true);
    generateCommitMessage(ws, sessionId)
      .then((result) => {
        setTitle(result.title);
        setDescription(result.description);
      })
      .catch((err) => {
        console.error("generateCommitMessage failed", err);
        toast.error("Failed to generate commit message");
      })
      .finally(() => setGenerating(false));
  };

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const trimmedTitle = title.trim();
    if (!trimmedTitle || loading) return;
    const trimmedDesc = description.trim();
    const message = trimmedDesc ? `${trimmedTitle}\n\n${trimmedDesc}` : trimmedTitle;
    onSubmit(message);
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <form onSubmit={handleSubmit}>
          <DialogHeader>
            <DialogTitle>Commit changes</DialogTitle>
          </DialogHeader>
          <div className="grid gap-3 py-4">
            <div className="relative">
              <Input
                placeholder="Commit title"
                value={title}
                onChange={(e) => setTitle(e.target.value)}
                autoFocus
                disabled={loading || generating}
              />
              {generating && (
                <div className="absolute right-2 top-1/2 -translate-y-1/2">
                  <Sparkles className="h-4 w-4 animate-pulse text-muted-foreground" />
                </div>
              )}
            </div>
            <div className="relative">
              <Textarea
                placeholder="Description (optional)"
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                rows={3}
                disabled={loading || generating}
              />
              {generating && (
                <div className="absolute right-2 top-2">
                  <Sparkles className="h-4 w-4 animate-pulse text-muted-foreground" />
                </div>
              )}
            </div>
          </div>
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={handleGenerate}
              disabled={loading || generating}
            >
              {generating ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <Sparkles className="h-3.5 w-3.5" />
              )}
              Generate
            </Button>
            <Button type="submit" size="sm" disabled={loading || generating || !title.trim()}>
              {loading && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              Commit
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
