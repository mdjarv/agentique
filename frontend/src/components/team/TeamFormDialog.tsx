import { Plus } from "lucide-react";
import type { ReactNode } from "react";
import { useCallback, useState } from "react";
import { toast } from "sonner";
import { Button } from "~/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "~/components/ui/dialog";
import { Input } from "~/components/ui/input";
import { Label } from "~/components/ui/label";
import { Textarea } from "~/components/ui/textarea";
import { useWebSocket } from "~/hooks/useWebSocket";
import type { TeamInfo } from "~/lib/team-actions";
import { createTeam, updateTeam } from "~/lib/team-actions";
import { getErrorMessage } from "~/lib/utils";

export function CreateTeamTrigger() {
  return (
    <Button variant="ghost" size="icon" className="size-6">
      <Plus className="size-3" />
    </Button>
  );
}

export function TeamFormDialog({ team, trigger }: { team?: TeamInfo; trigger: ReactNode }) {
  const ws = useWebSocket();
  const isEdit = !!team;
  const [open, setOpen] = useState(false);
  const [name, setName] = useState(team?.name ?? "");
  const [description, setDescription] = useState(team?.description ?? "");

  const handleSubmit = useCallback(async () => {
    try {
      if (isEdit && team) {
        await updateTeam(ws, { id: team.id, name, description });
      } else {
        await createTeam(ws, { name, description });
      }
      setOpen(false);
      if (!isEdit) {
        setName("");
        setDescription("");
      }
    } catch (e) {
      toast.error(getErrorMessage(e, "Operation failed"));
    }
  }, [ws, isEdit, team, name, description]);

  const handleOpenChange = useCallback(
    (next: boolean) => {
      setOpen(next);
      if (next && team) {
        setName(team.name);
        setDescription(team.description);
      }
    },
    [team],
  );

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogTrigger asChild>{trigger}</DialogTrigger>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{isEdit ? "Edit Team" : "New Team"}</DialogTitle>
          {!isEdit && (
            <DialogDescription>Create a persistent team for your agents.</DialogDescription>
          )}
        </DialogHeader>
        <div className="space-y-3">
          <div>
            <Label htmlFor="team-name">Name</Label>
            <Input
              id="team-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Core Team"
              autoFocus
            />
          </div>
          <div>
            <Label htmlFor="team-desc">Description</Label>
            <Textarea
              id="team-desc"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="Cross-project team for backend and frontend"
              rows={2}
            />
          </div>
        </div>
        <DialogFooter>
          <Button onClick={handleSubmit} disabled={!name.trim()}>
            {isEdit ? "Save" : "Create"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
