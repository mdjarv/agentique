import { Loader2, Sparkles, UserPlus } from "lucide-react";
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
import type { AgentProfileInfo } from "~/lib/team-actions";
import { createAgentProfile, generateAgentProfile, updateAgentProfile } from "~/lib/team-actions";
import { getErrorMessage } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";

export function ProfileEditorDialog({ profile }: { profile?: AgentProfileInfo }) {
  const ws = useWebSocket();
  const projects = useAppStore((s) => s.projects);
  const isEdit = !!profile;

  const [open, setOpen] = useState(false);
  const [name, setName] = useState(profile?.name ?? "");
  const [role, setRole] = useState(profile?.role ?? "");
  const [description, setDescription] = useState(profile?.description ?? "");
  const [projectId, setProjectId] = useState(profile?.projectId ?? "");
  const [avatar, setAvatar] = useState(profile?.avatar ?? "");
  const [generating, setGenerating] = useState(false);
  const [brief, setBrief] = useState("");
  const [showBrief, setShowBrief] = useState(false);

  const handleGenerate = useCallback(async () => {
    if (!projectId || generating) return;
    setGenerating(true);
    try {
      const result = await generateAgentProfile(ws, {
        projectId,
        brief: brief.trim() || undefined,
      });
      setName(result.name);
      setRole(result.role);
      setDescription(result.description);
      setAvatar(result.avatar);
    } catch (e) {
      toast.error(getErrorMessage(e, "Failed to generate profile"));
    } finally {
      setGenerating(false);
    }
  }, [ws, projectId, brief, generating]);

  const handleSave = useCallback(async () => {
    try {
      const params = {
        name,
        role,
        description,
        projectId,
        avatar,
        config: JSON.stringify(profile?.config ?? {}),
      };
      if (isEdit && profile) {
        await updateAgentProfile(ws, { id: profile.id, ...params });
      } else {
        await createAgentProfile(ws, params);
      }
      setOpen(false);
      if (!isEdit) {
        setName("");
        setRole("");
        setDescription("");
        setProjectId("");
        setAvatar("");
      }
    } catch (e) {
      toast.error(getErrorMessage(e, "Operation failed"));
    }
  }, [ws, isEdit, profile, name, role, description, projectId, avatar]);

  const handleOpenChange = useCallback(
    (nextOpen: boolean) => {
      setOpen(nextOpen);
      if (nextOpen && profile) {
        setName(profile.name);
        setRole(profile.role);
        setDescription(profile.description);
        setProjectId(profile.projectId);
        setAvatar(profile.avatar);
      }
      if (!nextOpen) {
        setBrief("");
        setShowBrief(false);
      }
    },
    [profile],
  );

  const trigger = isEdit ? (
    <button type="button" className="font-medium hover:underline text-left">
      {profile.name || "Unnamed"}
    </button>
  ) : (
    <Button variant="ghost" size="icon" className="size-6">
      <UserPlus className="size-3" />
    </Button>
  );

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogTrigger asChild>{trigger}</DialogTrigger>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{isEdit ? "Edit Agent Profile" : "New Agent Profile"}</DialogTitle>
          <DialogDescription>
            {isEdit
              ? "Update this agent's identity and configuration."
              : "Create a persistent agent identity."}
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-3">
          <div className="grid grid-cols-[1fr_80px] gap-3">
            <div>
              <Label htmlFor="profile-name">Name</Label>
              <Input
                id="profile-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="Backend Expert"
                autoFocus
              />
            </div>
            <div>
              <Label htmlFor="profile-avatar">Avatar</Label>
              <Input
                id="profile-avatar"
                value={avatar}
                onChange={(e) => setAvatar(e.target.value)}
                placeholder="🤖"
                className="text-center"
              />
            </div>
          </div>
          <div>
            <Label htmlFor="profile-role">Role</Label>
            <Input
              id="profile-role"
              value={role}
              onChange={(e) => setRole(e.target.value)}
              placeholder="backend architect"
            />
          </div>
          <div>
            <Label htmlFor="profile-desc">Description</Label>
            <Textarea
              id="profile-desc"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="Handles API endpoints, database migrations, and backend infrastructure."
              rows={3}
            />
          </div>
          <div>
            <Label htmlFor="profile-project">Home Project</Label>
            <select
              id="profile-project"
              className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-sm transition-colors"
              value={projectId}
              onChange={(e) => setProjectId(e.target.value)}
            >
              <option value="">None</option>
              {projects.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </select>
          </div>
          {projectId && (
            <div className="space-y-2">
              <div className="flex items-center gap-2">
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={handleGenerate}
                  disabled={generating}
                >
                  {generating ? (
                    <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  ) : (
                    <Sparkles className="h-3.5 w-3.5" />
                  )}
                  {generating ? "Generating..." : "Generate from project"}
                </Button>
                <button
                  type="button"
                  className="text-xs text-muted-foreground hover:text-foreground"
                  onClick={() => setShowBrief((v) => !v)}
                >
                  {showBrief ? "Hide brief" : "+ Add brief"}
                </button>
              </div>
              {showBrief && (
                <Input
                  value={brief}
                  onChange={(e) => setBrief(e.target.value)}
                  placeholder="e.g. Focus on API endpoints and database migrations"
                  className="text-xs"
                  disabled={generating}
                />
              )}
            </div>
          )}
        </div>
        <DialogFooter>
          <Button onClick={handleSave} disabled={!name.trim() || generating}>
            {isEdit ? "Save" : "Create"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
