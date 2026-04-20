import { Gauge, Loader2, ShieldAlert, ShieldCheck, Sparkles } from "lucide-react";
import type { ReactNode } from "react";
import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import { ToolbarDropdown, type ToolbarDropdownOption } from "~/components/chat/ToolbarDropdown";
import { Button } from "~/components/ui/button";
import { Input } from "~/components/ui/input";
import { Label } from "~/components/ui/label";
import { Popover, PopoverContent, PopoverTrigger } from "~/components/ui/popover";
import { Textarea } from "~/components/ui/textarea";
import { useWebSocket } from "~/hooks/useWebSocket";
import { listPresetDefinitions } from "~/lib/api";
import {
  EFFORT_COLORS,
  EFFORT_LABELS,
  EFFORT_LEVELS,
  type EffortLevel,
  PERMISSION_BG,
  PERMISSION_COLORS,
  PERMISSION_DESCRIPTIONS,
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

// ─── Option lists ──────────────────────────────────────

const MODEL_OPTIONS: ToolbarDropdownOption[] = [
  { value: "", label: "Project default" },
  ...MODELS.map((m) => ({ value: m, label: MODEL_LABELS[m] })),
];

const EFFORT_OPTIONS: ToolbarDropdownOption[] = [
  { value: "", label: "Project default" },
  ...EFFORT_LEVELS.filter((l): l is Exclude<EffortLevel, ""> => l !== "").map((l) => ({
    value: l,
    label: EFFORT_LABELS[l],
    color: EFFORT_COLORS[l],
  })),
];

const PERMISSION_OPTIONS: ToolbarDropdownOption[] = [
  { value: "", label: "Project default" },
  ...PERMISSION_MODES.map((m) => ({
    value: m,
    label: PERMISSION_LABELS[m],
    icon:
      m === "fullAuto" ? <ShieldAlert className="h-3 w-3" /> : <ShieldCheck className="h-3 w-3" />,
    color: PERMISSION_COLORS[m],
    description: PERMISSION_DESCRIPTIONS[m],
  })),
];

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

const DEFAULT_AVATAR = "🤖";

// ─── Small presentational helpers ──────────────────────

function AvatarPicker({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  const [open, setOpen] = useState(false);
  const [custom, setCustom] = useState("");
  const displayed = value || DEFAULT_AVATAR;
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button
          type="button"
          className={cn(
            "flex h-9 w-9 items-center justify-center rounded-md border border-input bg-transparent text-lg shadow-sm transition-colors hover:bg-muted/50",
            !value && "opacity-60",
          )}
          aria-label="Pick avatar"
          title={value ? "Change avatar" : "Default avatar — click to change"}
        >
          {displayed}
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
  return <p className="mt-1.5 text-[11px] text-muted-foreground-faint">{children}</p>;
}

function Field({ children, className }: { children: ReactNode; className?: string }) {
  return <div className={cn("space-y-1.5", className)}>{children}</div>;
}

// ─── Main form ─────────────────────────────────────────

export interface ProfileFormProps {
  profile?: AgentProfileInfo;
  /** Called after a successful save with the saved profile. */
  onSaved?: (profile: AgentProfileInfo) => void;
  onCancel?: () => void;
}

export function ProfileForm({ profile, onSaved, onCancel }: ProfileFormProps) {
  const ws = useWebSocket();
  const projects = useAppStore((s) => s.projects);
  const isEdit = !!profile;

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
    listPresetDefinitions()
      .then(setPresetDefs)
      .catch((err) => console.error("listPresetDefinitions failed", err));
  }, []);

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
      let saved: AgentProfileInfo;
      if (isEdit && profile) {
        saved = await updateAgentProfile(ws, { id: profile.id, ...params });
        useTeamStore.getState().updateProfile(saved);
      } else {
        saved = await createAgentProfile(ws, params);
        useTeamStore.getState().addProfile(saved);
      }
      if (!isEdit) {
        setName("");
        setRole("");
        setDescription("");
        setProjectId("");
        setAvatar("");
        setConfig(emptyPersonaConfig());
      }
      onSaved?.(saved);
    } catch (e) {
      toast.error(getErrorMessage(e, "Operation failed"));
    } finally {
      setSaving(false);
    }
  }, [ws, isEdit, profile, name, role, description, projectId, avatar, config, onSaved]);

  const bp = config.behaviorPresets ?? DEFAULT_PRESETS;

  return (
    <div className="mx-auto w-full max-w-5xl px-6 py-8 space-y-8">
      <div className="grid grid-cols-1 md:grid-cols-2 gap-8">
        {/* ── Identity column ───────────────────────── */}
        <div className="space-y-5">
          <SectionHeading
            title="Identity"
            hint="Name and role reach the agent through its session preamble. Description and avatar are display-only."
          />

          <div className="flex items-end gap-3">
            <Field className="flex-1">
              <Label htmlFor="profile-name">Name</Label>
              <Input
                id="profile-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="Backend Expert"
                autoFocus={!isEdit}
              />
            </Field>
            <Field>
              <Label>Avatar</Label>
              <AvatarPicker value={avatar} onChange={setAvatar} />
            </Field>
          </div>

          <Field>
            <Label htmlFor="profile-role">Role</Label>
            <Input
              id="profile-role"
              value={role}
              onChange={(e) => setRole(e.target.value)}
              placeholder="backend architect"
            />
          </Field>

          <Field>
            <Label htmlFor="profile-desc">Description</Label>
            <Textarea
              id="profile-desc"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="Handles API endpoints, database migrations, and backend infrastructure."
              rows={3}
            />
          </Field>

          <Field>
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
          </Field>

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
        <div className="space-y-5">
          <SectionHeading
            title="Session defaults"
            hint="Applied when launching a session from this profile. Explicit launch overrides still win."
          />

          <div className="grid grid-cols-3 gap-3">
            <Field>
              <Label className="text-xs text-muted-foreground font-normal">Model</Label>
              <ToolbarDropdown
                value={config.model ?? ""}
                onChange={(v) => setConfig((c) => ({ ...c, model: v }))}
                options={MODEL_OPTIONS}
              />
            </Field>
            <Field>
              <Label className="text-xs text-muted-foreground font-normal">Effort</Label>
              <ToolbarDropdown
                value={config.effort ?? ""}
                onChange={(v) => setConfig((c) => ({ ...c, effort: v }))}
                options={EFFORT_OPTIONS}
                icon={<Gauge className="h-3 w-3" />}
                triggerColor={
                  config.effort ? EFFORT_COLORS[config.effort as EffortLevel] : undefined
                }
              />
            </Field>
            <Field>
              <Label className="text-xs text-muted-foreground font-normal">Permission</Label>
              <ToolbarDropdown
                value={config.autoApproveMode ?? ""}
                onChange={(v) => setConfig((c) => ({ ...c, autoApproveMode: v }))}
                options={PERMISSION_OPTIONS}
                icon={
                  config.autoApproveMode === "fullAuto" ? (
                    <ShieldAlert className="h-3 w-3" />
                  ) : config.autoApproveMode ? (
                    <ShieldCheck className="h-3 w-3" />
                  ) : undefined
                }
                triggerColor={
                  config.autoApproveMode
                    ? PERMISSION_COLORS[config.autoApproveMode as AutoApproveMode]
                    : undefined
                }
                triggerBgColor={
                  config.autoApproveMode
                    ? PERMISSION_BG[config.autoApproveMode as AutoApproveMode]
                    : undefined
                }
              />
            </Field>
          </div>

          <Field>
            <Label htmlFor="profile-system-prompt">System prompt additions</Label>
            <Textarea
              id="profile-system-prompt"
              value={config.systemPromptAdditions ?? ""}
              onChange={(e) => setConfig((c) => ({ ...c, systemPromptAdditions: e.target.value }))}
              placeholder="You are a senior backend architect. Prioritize correctness over speed..."
              rows={6}
            />
            <Helper>Appended to every session preamble. Use for persistent role context.</Helper>
          </Field>

          <Field>
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
                      <div className="text-xs text-muted-foreground mt-0.5">{def.description}</div>
                    </div>
                  </button>
                );
              })}
            </div>
            <Field>
              <Label
                htmlFor="profile-custom-instructions"
                className="text-xs text-muted-foreground font-normal"
              >
                Custom instructions
              </Label>
              <Textarea
                id="profile-custom-instructions"
                value={bp.customInstructions ?? ""}
                onChange={(e) => setCustomInstructions(e.target.value)}
                placeholder="Additional preset instructions (e.g., 'only touch backend files')..."
                rows={2}
              />
            </Field>
          </Field>
        </div>
      </div>

      <div className="flex items-center justify-end gap-2 border-t pt-6">
        {onCancel && (
          <Button type="button" variant="ghost" onClick={onCancel} disabled={saving}>
            Cancel
          </Button>
        )}
        <Button onClick={handleSave} disabled={!name.trim() || generating || saving}>
          {saving ? "Saving..." : isEdit ? "Save changes" : "Create profile"}
        </Button>
      </div>
    </div>
  );
}
