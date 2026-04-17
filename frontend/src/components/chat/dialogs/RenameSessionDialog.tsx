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
import { useWebSocket } from "~/hooks/useWebSocket";
import { generateSessionName } from "~/lib/session/actions";
import { getErrorMessage } from "~/lib/utils";

interface RenameSessionDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  sessionId: string;
  currentName: string;
  onSubmit: (name: string) => void;
  saving: boolean;
}

export function RenameSessionDialog({
  open,
  onOpenChange,
  sessionId,
  currentName,
  onSubmit,
  saving,
}: RenameSessionDialogProps) {
  const ws = useWebSocket();
  const [name, setName] = useState("");
  const [generating, setGenerating] = useState(false);

  useEffect(() => {
    if (open) setName(currentName);
  }, [open, currentName]);

  const handleGenerate = () => {
    setGenerating(true);
    generateSessionName(ws, sessionId)
      .then((result) => setName(result.name))
      .catch((err) => {
        toast.error(getErrorMessage(err, "Failed to generate name"));
      })
      .finally(() => setGenerating(false));
  };

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const trimmed = name.trim();
    if (!trimmed || saving) return;
    onSubmit(trimmed);
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <form onSubmit={handleSubmit}>
          <DialogHeader>
            <DialogTitle>Rename session</DialogTitle>
          </DialogHeader>
          <div className="grid gap-3 py-4">
            <div className="relative">
              <Input
                placeholder="Session name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                autoFocus
                disabled={saving || generating}
                maxLength={200}
              />
              {generating && (
                <div className="absolute right-2 top-1/2 -translate-y-1/2">
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
              disabled={saving || generating}
              title="Generate a name from the session transcript"
            >
              {generating ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <Sparkles className="h-3.5 w-3.5" />
              )}
              Generate
            </Button>
            <Button
              type="submit"
              size="sm"
              disabled={saving || generating || !name.trim() || name.trim() === currentName}
            >
              {saving && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              Rename
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
