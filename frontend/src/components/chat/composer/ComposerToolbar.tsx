import { Paperclip } from "lucide-react";
import { memo } from "react";
import { BrainControl } from "~/components/chat/composer/BrainControl";
import { PermissionMark } from "~/components/chat/composer/PermissionMark";
import type { EffortLevel } from "~/lib/composer-constants";
import type { ModelId, ProviderId } from "~/lib/model-catalog";
import type { AutoApproveMode } from "~/stores/chat-store";

interface ComposerToolbarProps {
  attachmentsSupported: boolean;
  onAttachClick: () => void;
  /** Disables the attach button (disabled || submitting). */
  disabled: boolean;
  templatePicker?: React.ReactNode;
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
  /** Narrow pane: the model name drops to its meter. */
  compact?: boolean;
}

/**
 * The left-hand controls of the composer's bottom bar.
 *
 * Two groups, in the order the decision is made: things that go *into* the
 * message (attach, templates) and the thing that answers it (the brain, and the
 * one mark saying how freely it may act). Attach keeps the leading position —
 * it is the only control reached for mid-sentence.
 *
 * What is gone: the Chat/Plan toggle, which nobody used, and the labelled
 * permission dropdown, which spent ~92px every session on a word that never
 * changed. Plan *review* is untouched — `PlanReviewBanner` runs off an approval
 * the CLI raises when the agent exits plan mode itself, not off a toggle here.
 *
 * Memoized AND rendered through a stable element prop from the shell, so typing
 * (which never changes any of these props) does not re-render this subtree.
 */
export const ComposerToolbar = memo(function ComposerToolbar({
  attachmentsSupported,
  onAttachClick,
  disabled,
  templatePicker,
  autoApproveMode,
  onAutoApproveModeChange,
  provider,
  onProviderChange,
  model,
  modelDisplayName,
  onModelChange,
  effort,
  onEffortChange,
  compact = false,
}: ComposerToolbarProps) {
  const hasSettings = !!model || autoApproveMode !== undefined;

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

      {hasSettings && <div className="w-px h-4 bg-border mx-1 shrink-0" />}

      <BrainControl
        model={model}
        modelDisplayName={modelDisplayName}
        onModelChange={onModelChange}
        provider={provider}
        onProviderChange={onProviderChange}
        effort={effort}
        onEffortChange={onEffortChange}
        compact={compact}
      />
      {autoApproveMode !== undefined && (
        <PermissionMark mode={autoApproveMode} onChange={onAutoApproveModeChange} />
      )}
    </div>
  );
});
