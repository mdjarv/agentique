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
  buildModelOptions,
  type ModelId,
  type ProviderId,
  providerForModel,
} from "~/lib/model-catalog";
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

const EFFORT_OPTIONS: ToolbarDropdownOption[] = EFFORT_LEVELS.map((lvl) => ({
  value: lvl,
  label: EFFORT_LABELS[lvl],
  color: EFFORT_COLORS[lvl],
}));

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
  modelDisplayName?: string;
  onModelChange?: (value: ModelId) => void;
  effort?: EffortLevel;
  onEffortChange?: (value: EffortLevel) => void;
  /**
   * What the New-session panel opened with, carried over from the last session
   * created. Marked in the model and effort dropdowns so a deviation from the
   * habit is visible and reversible. Absent everywhere else: an existing
   * session's model is its own fact, not a memory of the last one started.
   */
  lastUsed?: { model: ModelId; effort: EffortLevel };
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
  modelDisplayName,
  onModelChange,
  effort,
  onEffortChange,
  lastUsed,
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
          selectedLabel={modelDisplayName}
          onChange={
            onModelChange
              ? (v) => {
                  const next = v as ModelId;
                  const nextProvider = providerOf(v) ?? providerForModel(next);
                  if (nextProvider && nextProvider !== provider) {
                    onProviderChange?.(nextProvider);
                  }
                  onModelChange(next);
                }
              : undefined
          }
          options={modelOptions}
          lastUsedValue={lastUsed?.model}
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
          lastUsedValue={lastUsed?.effort}
        />
      )}
    </div>
  );
});
