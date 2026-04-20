import { Loader2, Sparkles, UserPlus } from "lucide-react";
import type { ReactNode } from "react";
import { useCallback, useEffect, useState } from "react";
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
import { Popover, PopoverContent, PopoverTrigger } from "~/components/ui/popover";
import { Textarea } from "~/components/ui/textarea";
import { useWebSocket } from "~/hooks/useWebSocket";
import { listPresetDefinitions } from "~/lib/api";
import {
  EFFORT_LABELS,
  EFFORT_LEVELS,
  type EffortLevel,
  PERMISSION_LABELS,
  PERMISSION_MODES,
} from "~/lib/composer-constants";
import type { BehaviorPresets, PresetDefinition } from "~/lib/generated-types";
import {
  DEFAULT_PRESETS,
  emptyPersonaConfig,
  hydratePersonaConfig,
  stripPersonaConfig,
} from "~/lib/persona-config";
import { MODEL_LABELS, MODELS } from "~/lib/session/actions";
import type { AgentProfileConfig, AgentProfileInfo } from "~/lib/team-actions";
import { createAgentProfile, generateAgentProfile, updateAgentProfile } from "~/lib/team-actions";
import { cn, getErrorMessage } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";
import type { AutoApproveMode } from "~/stores/chat-store";
import { useTeamStore } from "~/stores/team-store";

const AVATAR_EMOJI = [
  "🤖",
  "🧠",
  "🛠️",
  "🔧",
  "🧪",
  "🧬",
  "🔍",
  "📝",
  "📊",
  "🎨",
  "🖼️",
  "🚀",
  "⚡",
  "🛡️",
  "💻",
  "🗄️",
  "🎯",
  "🧙",
  "🦉",
  "🦊",
  "🐙",
  "🐝",
  "🐳",
  "🦖",
];

function AvatarPicker({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  const [open, setOpen] = useState(false);
  const [custom, setCustom] = useState("");
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button
          type="button"
          className="flex h-9 w-9 items-center justify-center rounded-md border border-input bg-transparent text-lg shadow-sm hover:bg-muted/50"
          aria-label="Pick avatar"
        >
          {value || <span className="text-xs text-muted-foreground">pick</span>}
        </button>
      </PopoverTrigger>
      <PopoverContent className="w-64 p-3" align="start">
        <div className="grid grid-cols-6 gap-1">
          {AVATAR_EMOJI.map((e) => (
            <button
              key={e}
              type="button"
              onClick={() => {
                onChange(e);
                setOpen(false);
              }}
              className={cn(
                "flex h-8 w-8 items-center justify-center rounded text-lg hover:bg-muted",
                value === e && "bg-muted ring-1 ring-primary",
              )}
            >
              {e}
            </button>
          ))}
        </div>
        <div className="mt-3 flex items-center gap-1.5">
          <Input
            placeholder="Custom emoji"
            value={custom}
            onChange={(ev) => setCustom(ev.target.value)}
            className="h-7 text-sm"
            maxLength={4}
          />
          <Button
            type="button"
            size="sm"
            variant="outline"
            className="h-7"
            disabled={!custom.trim()}
            onClick={() => {
              onChange(custom.trim());
              setCustom("");
              setOpen(false);
            }}
          >
            Set
          </Button>
        </div>
        {value && (
          <button
            type="button"
            onClick={() => {
              onChange("");
              setOpen(false);
            }}
            className="mt-2 text-xs text-muted-foreground hover:text-foreground"
          >
            Clear avatar
          </button>
        )}
      </PopoverContent>
    </Popover>
  );
}

function SectionHeading({ title, hint }: { title: string; hint: string }) {
  return (
    <div className="space-y-1">
      <h3 className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
        {title}
      </h3>
      <p className="text-xs text-muted-foreground-faint leading-snug">{hint}</p>
    </div>
  );
}

function Helper({ children }: { children: ReactNode }) {
  return <p className="mt-1 text-[11px] text-muted-foreground-faint">{children}</p>;
}

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
  const [config, setConfig] = useState<AgentProfileConfig>(() =>
    hydratePersonaConfig(profile?.config),
  );
  const [generating, setGenerating] = useState(false);
  const [brief, setBrief] = useState("");
  const [showBrief, setShowBrief] = useState(false);
  const [saving, setSaving] = useState(false);
  const [presetDefs, setPresetDefs] = useState<PresetDefinition[]>([]);

  useEffect(() => {
    if (!open || presetDefs.length > 0) return;
    listPresetDefinitions()
      .then(setPresetDefs)
      .catch((err) => console.error("listPresetDefinitions failed", err));
  }, [open, presetDefs.length]);

  const togglePreset = useCallback((key: keyof BehaviorPresets) => {
    setConfig((prev) => ({
      ...prev,
      behaviorPresets: {
        ...DEFAULT_PRESETS,
        ...prev.behaviorPresets,
        [key]: !prev.behaviorPresets?.[key],
      },
    }));
  }, []);

  const setCustomInstructions = useCallback((value: string) => {
    setConfig((prev) => ({
      ...prev,
      behaviorPresets: {
        ...DEFAULT_PRESETS,
        ...prev.behaviorPresets,
        customInstructions: value,
      },
    }));
  }, []);

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
    setSaving(true);
    try {
      const params = {
        name,
        role,
        description,
        projectId,
        avatar,
        config: JSON.stringify(stripPersonaConfig(config)),
      };
      if (isEdit && profile) {
        const updated = await updateAgentProfile(ws, { id: profile.id, ...params });
        useTeamStore.getState().updateProfile(updated);
      } else {
        const created = await createAgentProfile(ws, params);
        useTeamStore.getState().addProfile(created);
      }
      setOpen(false);
      if (!isEdit) {
        setName("");
        setRole("");
        setDescription("");
        setProjectId("");
        setAvatar("");
        setConfig(emptyPersonaConfig());
      }
    } catch (e) {
      toast.error(getErrorMessage(e, "Operation failed"));
    } finally {
      setSaving(false);
    }
  }, [ws, isEdit, profile, name, role, description, projectId, avatar, config]);

  const handleOpenChange = useCallback(
    (nextOpen: boolean) => {
      setOpen(nextOpen);
      if (nextOpen && profile) {
        setName(profile.name);
        setRole(profile.role);
        setDescription(profile.description);
        setProjectId(profile.projectId);
        setAvatar(profile.avatar);
        setConfig(hydratePersonaConfig(profile.config));
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

  const bp = config.behaviorPresets ?? DEFAULT_PRESETS;

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogTrigger asChild>{trigger}</DialogTrigger>
      <DialogContent className="sm:max-w-3xl max-h-[85vh] overflow-hidden flex flex-col">
        <DialogHeader>
          <DialogTitle>{isEdit ? "Edit Agent Profile" : "New Agent Profile"}</DialogTitle>
          <DialogDescription>
            {isEdit
              ? "Update this agent's identity and session defaults."
              : "Create a persistent agent identity with session defaults."}
          </DialogDescription>
        </DialogHeader>
        <div className="grid grid-cols-1 md:grid-cols-2 gap-6 overflow-y-auto pr-1 -mr-1">
          {/* ── Identity column ───────────────────────── */}
          <div className="space-y-4">
            <SectionHeading
              title="Identity"
              hint="Name and role reach the agent through its session preamble. Description and avatar are display-only."
            />

            <div className="flex items-end gap-3">
              <div className="flex-1">
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
                <Label>Avatar</Label>
                <AvatarPicker value={avatar} onChange={setAvatar} />
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
              <Helper>Sessions launched from this profile start in this project's worktree.</Helper>
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

          {/* ── Behavior column ───────────────────────── */}
          <div className="space-y-4">
            <SectionHeading
              title="Session defaults"
              hint="Applied when launching a session from this profile. Explicit launch overrides still win."
            />

            <div className="grid grid-cols-2 gap-3">
              <div>
                <Label htmlFor="profile-model">Model</Label>
                <select
                  id="profile-model"
                  className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-sm"
                  value={config.model ?? ""}
                  onChange={(e) => setConfig((c) => ({ ...c, model: e.target.value }))}
                >
                  <option value="">Project default</option>
                  {MODELS.map((m) => (
                    <option key={m} value={m}>
                      {MODEL_LABELS[m]}
                    </option>
                  ))}
                </select>
              </div>
              <div>
                <Label htmlFor="profile-effort">Effort</Label>
                <select
                  id="profile-effort"
                  className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-sm"
                  value={config.effort ?? ""}
                  onChange={(e) => setConfig((c) => ({ ...c, effort: e.target.value }))}
                >
                  <option value="">Project default</option>
                  {EFFORT_LEVELS.filter((l): l is Exclude<EffortLevel, ""> => l !== "").map((l) => (
                    <option key={l} value={l}>
                      {EFFORT_LABELS[l]}
                    </option>
                  ))}
                </select>
              </div>
            </div>

            <div>
              <Label htmlFor="profile-autoapprove">Permission mode</Label>
              <select
                id="profile-autoapprove"
                className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-sm"
                value={config.autoApproveMode ?? ""}
                onChange={(e) => setConfig((c) => ({ ...c, autoApproveMode: e.target.value }))}
              >
                <option value="">Project default</option>
                {PERMISSION_MODES.map((m: AutoApproveMode) => (
                  <option key={m} value={m}>
                    {PERMISSION_LABELS[m]}
                  </option>
                ))}
              </select>
            </div>

            <div className="space-y-2">
              <Label>Behavior presets</Label>
              <div className="space-y-1.5">
                {presetDefs.map((def) => {
                  const key = def.key as keyof BehaviorPresets;
                  const active = !!bp[key];
                  return (
                    <button
                      key={def.key}
                      type="button"
                      onClick={() => togglePreset(key)}
                      className="flex items-start gap-3 w-full text-left rounded-lg border px-3 py-2 transition-colors hover:bg-muted/50"
                    >
                      <div
                        className={cn(
                          "mt-0.5 h-5 w-9 flex-shrink-0 rounded-full transition-colors relative",
                          active ? "bg-primary" : "bg-muted-foreground/30",
                        )}
                      >
                        <div
                          className={cn(
                            "absolute top-0.5 h-4 w-4 rounded-full bg-white transition-transform",
                            active ? "translate-x-4" : "translate-x-0.5",
                          )}
                        />
                      </div>
                      <div className="min-w-0">
                        <div className="text-sm font-medium">{def.title}</div>
                        <div className="text-xs text-muted-foreground mt-0.5">
                          {def.description}
                        </div>
                      </div>
                    </button>
                  );
                })}
              </div>
            </div>

            <div>
              <Label htmlFor="profile-custom-instructions">Custom instructions</Label>
              <Textarea
                id="profile-custom-instructions"
                value={bp.customInstructions ?? ""}
                onChange={(e) => setCustomInstructions(e.target.value)}
                placeholder="Additional preset instructions (e.g., 'only touch backend files')..."
                rows={2}
              />
            </div>

            <div>
              <Label htmlFor="profile-system-prompt">System prompt additions</Label>
              <Textarea
                id="profile-system-prompt"
                value={config.systemPromptAdditions ?? ""}
                onChange={(e) =>
                  setConfig((c) => ({ ...c, systemPromptAdditions: e.target.value }))
                }
                placeholder="You are a senior backend architect. Prioritize correctness over speed..."
                rows={4}
              />
              <Helper>Appended to every session preamble. Use for persistent role context.</Helper>
            </div>
          </div>
        </div>
        <DialogFooter>
          <Button onClick={handleSave} disabled={!name.trim() || generating || saving}>
            {saving ? "Saving..." : isEdit ? "Save" : "Create"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
