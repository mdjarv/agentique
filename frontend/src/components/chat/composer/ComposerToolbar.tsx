import {
  FolderOpen,
  Gauge,
  GitBranch,
  ListChecks,
  MessageSquare,
  Paperclip,
  ShieldAlert,
  ShieldCheck,
} from "lucide-react";
import { memo, useMemo } from "react";
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
import {
  MODEL_LABELS,
  MODEL_PROVIDER,
  MODELS,
  type ModelId,
  PROVIDER_LABELS,
  PROVIDERS,
  type ProviderId,
} from "~/lib/session/actions";
import type { AutoApproveMode } from "~/stores/chat-store";
import { useProviderStore } from "~/stores/provider-store";
import { ToolbarDropdown, type ToolbarDropdownOption } from "../ToolbarDropdown";
import { ToolbarToggle } from "../ToolbarToggle";

const PERMISSION_OPTIONS: ToolbarDropdownOption[] = PERMISSION_MODES.map((m) => ({
  value: m,
  label: PERMISSION_LABELS[m],
  icon:
    m === "fullAuto" ? <ShieldAlert className="h-3 w-3" /> : <ShieldCheck className="h-3 w-3" />,
  color: PERMISSION_COLORS[m],
  description: PERMISSION_DESCRIPTIONS[m],
}));

/** Static fallback options used until the backend catalog hydrates. */
const STATIC_MODEL_OPTIONS: ToolbarDropdownOption[] = MODELS.map((m) => ({
  value: m,
  label: MODEL_LABELS[m],
  group: PROVIDER_LABELS[MODEL_PROVIDER[m]],
}));

const EFFORT_OPTIONS: ToolbarDropdownOption[] = EFFORT_LEVELS.map((lvl) => ({
  value: lvl,
  label: EFFORT_LABELS[lvl],
  color: EFFORT_COLORS[lvl],
}));

interface BuiltOptions {
  options: ToolbarDropdownOption[];
  /** model slug → provider, built from whatever catalog is available. */
  providerOf: (slug: string) => ProviderId | undefined;
}

function buildModelOptions(
  catalog: Record<string, { slug: string; displayName: string; description?: string }[]>,
  filterProvider: ProviderId | undefined,
): BuiltOptions {
  const providers = filterProvider ? [filterProvider] : PROVIDERS;
  const providerOf = new Map<string, ProviderId>();
  const options: ToolbarDropdownOption[] = [];

  for (const p of providers) {
    const groupLabel = PROVIDER_LABELS[p];
    const dynamic = catalog[p];
    const entries =
      dynamic && dynamic.length > 0
        ? dynamic.map((m) => ({
            value: m.slug,
            label: m.displayName || m.slug,
            description: m.description,
          }))
        : STATIC_MODEL_OPTIONS.filter((o) => MODEL_PROVIDER[o.value as ModelId] === p).map(
            ({ value, label }) => ({ value, label, description: undefined as string | undefined }),
          );

    for (const entry of entries) {
      providerOf.set(entry.value, p);
      options.push({
        value: entry.value,
        label: entry.label,
        group: groupLabel,
        ...(entry.description ? { description: entry.description } : {}),
      });
    }
  }

  return { options, providerOf: (slug) => providerOf.get(slug) };
}

interface ComposerToolbarProps {
  attachmentsSupported: boolean;
  onAttachClick: () => void;
  /** Disables the attach button (disabled || submitting). */
  disabled: boolean;
  templatePicker?: React.ReactNode;
  worktree?: boolean;
  onWorktreeChange?: (value: boolean) => void;
  planMode?: boolean;
  onPlanModeChange?: (value: boolean) => void;
  isRunning?: boolean;
  autoApproveMode?: AutoApproveMode;
  onAutoApproveModeChange?: (value: AutoApproveMode) => void;
  provider?: ProviderId;
  onProviderChange?: (value: ProviderId) => void;
  model?: ModelId;
  onModelChange?: (value: ModelId) => void;
  effort?: EffortLevel;
  onEffortChange?: (value: EffortLevel) => void;
}

/**
 * The left-hand controls of the composer's bottom bar (attach, template, mode
 * toggles, model/effort/permission dropdowns).
 *
 * Memoized AND rendered through a stable element prop from the shell, so typing
 * (which never changes any of these props) does not re-render this subtree. The
 * model catalog is read here and `buildModelOptions` runs behind `useMemo`, so a
 * keystroke no longer reallocates a Map + arrays the way it did when this lived
 * inline in the composer body.
 */
export const ComposerToolbar = memo(function ComposerToolbar({
  attachmentsSupported,
  onAttachClick,
  disabled,
  templatePicker,
  worktree,
  onWorktreeChange,
  planMode,
  onPlanModeChange,
  isRunning,
  autoApproveMode,
  onAutoApproveModeChange,
  provider,
  onProviderChange,
  model,
  onModelChange,
  effort,
  onEffortChange,
}: ComposerToolbarProps) {
  const catalog = useProviderStore((s) => s.models);
  const { options: modelOptions, providerOf } = useMemo(
    () => buildModelOptions(catalog, provider),
    [catalog, provider],
  );

  const showWorktreeToggle = worktree !== undefined && !!onWorktreeChange;
  const showEffortDropdown = effort !== undefined && !!onEffortChange;
  const hasToggles = showWorktreeToggle || !!onPlanModeChange || autoApproveMode !== undefined;
  const mode = autoApproveMode ?? "manual";

  return (
    <div className="flex items-center gap-0.5 max-md:gap-1 max-md:overflow-x-auto max-md:flex-nowrap min-w-0">
      {attachmentsSupported && (
        <button
          type="button"
          onClick={onAttachClick}
          disabled={disabled}
          className="h-7 w-7 max-md:h-10 max-md:w-10 rounded-lg text-muted-foreground hover:text-foreground hover:bg-muted/80 flex items-center justify-center transition-colors disabled:opacity-40 cursor-pointer"
          aria-label="Attach files"
        >
          <Paperclip className="h-3.5 w-3.5" />
        </button>
      )}
      {templatePicker}

      {hasToggles && <div className="w-px h-4 bg-border mx-1 shrink-0" />}

      {showWorktreeToggle && (
        <ToolbarToggle
          active={worktree ?? false}
          onChange={onWorktreeChange}
          activeIcon={<GitBranch className="h-3 w-3" />}
          inactiveIcon={<FolderOpen className="h-3 w-3" />}
          activeLabel="Worktree"
          inactiveLabel="Local"
          activeColor="bg-primary/10 text-primary"
          inactiveColor="bg-orange/10 text-orange"
        />
      )}
      {onPlanModeChange && (
        <ToolbarToggle
          active={planMode ?? false}
          onChange={onPlanModeChange}
          activeIcon={<ListChecks className="h-3 w-3" />}
          inactiveIcon={<MessageSquare className="h-3 w-3" />}
          activeLabel="Plan"
          inactiveLabel="Chat"
          activeColor="bg-warning/10 text-warning"
          inactiveColor="bg-primary/10 text-primary"
          disabled={isRunning}
        />
      )}
      {autoApproveMode !== undefined && (
        <ToolbarDropdown
          value={mode}
          onChange={
            onAutoApproveModeChange
              ? (v) => onAutoApproveModeChange(v as AutoApproveMode)
              : undefined
          }
          options={PERMISSION_OPTIONS}
          icon={
            mode === "fullAuto" ? (
              <ShieldAlert className="h-3 w-3" />
            ) : (
              <ShieldCheck className="h-3 w-3" />
            )
          }
          triggerColor={PERMISSION_COLORS[mode]}
          triggerBgColor={PERMISSION_BG[mode]}
          readOnlyColor={PERMISSION_COLORS[mode]}
        />
      )}

      {(showEffortDropdown || model) && <div className="w-px h-4 bg-border mx-1 shrink-0" />}

      {model && (
        <ToolbarDropdown
          value={model}
          onChange={
            onModelChange
              ? (v) => {
                  const next = v as ModelId;
                  const nextProvider = providerOf(v) ?? MODEL_PROVIDER[next];
                  if (nextProvider && nextProvider !== provider) {
                    onProviderChange?.(nextProvider);
                  }
                  onModelChange(next);
                }
              : undefined
          }
          options={modelOptions}
        />
      )}
      {showEffortDropdown && effort !== undefined && (
        <ToolbarDropdown
          value={effort}
          onChange={onEffortChange ? (v) => onEffortChange(v as EffortLevel) : undefined}
          options={EFFORT_OPTIONS}
          icon={<Gauge className="h-3 w-3" />}
          triggerColor={EFFORT_COLORS[effort]}
          readOnlyColor={EFFORT_COLORS[effort]}
        />
      )}
    </div>
  );
});
