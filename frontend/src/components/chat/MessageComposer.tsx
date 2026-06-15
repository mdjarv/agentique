import { ClipboardPaste, ListPlus, Mic, MicOff, SendHorizonal, Square } from "lucide-react";
import { forwardRef, useCallback, useImperativeHandle, useRef, useState } from "react";
import { useAttachments } from "~/hooks/useAttachments";
import { ACCEPTED_TYPES, type EffortLevel } from "~/lib/composer-constants";
import type { ModelId, ProviderId } from "~/lib/session/actions";
import { cn } from "~/lib/utils";
import type { Attachment, AutoApproveMode } from "~/stores/chat-store";
import { AttachmentStrip } from "./composer/AttachmentStrip";
import { ComposerTextarea, type ComposerTextareaHandle } from "./composer/ComposerTextarea";
import { ComposerToolbar } from "./composer/ComposerToolbar";
import { useComposerSend } from "./composer/useComposerSend";
import { useComposerSpeech } from "./composer/useComposerSpeech";
import { ImageLightbox } from "./ImageLightbox";

export type { EffortLevel };

export interface ComposerHandle {
  setText: (text: string) => void;
}

type SendResult = boolean | undefined;

interface MessageComposerProps {
  projectId: string;
  onSend: (prompt: string, attachments?: Attachment[]) => SendResult | Promise<SendResult>;
  disabled?: boolean;
  isRunning?: boolean;
  onInterrupt?: () => void;
  initialText?: string;
  onTextPersist?: (text: string) => void;
  placeholder?: string;
  worktree?: boolean;
  onWorktreeChange?: (value: boolean) => void;
  planMode?: boolean;
  onPlanModeChange?: (value: boolean) => void;
  autoApproveMode?: AutoApproveMode;
  onAutoApproveModeChange?: (value: AutoApproveMode) => void;
  /**
   * Locks the model picker to a single provider's models. Required for running
   * sessions (mid-session provider switching is not supported). Leave undefined
   * on the new-session form to allow picking across all providers; the parent
   * derives the provider from the selected model via `onProviderChange`.
   */
  provider?: ProviderId;
  /** Called when the picked model implies a different provider than the current one. */
  onProviderChange?: (value: ProviderId) => void;
  attachmentsSupported?: boolean;
  model?: ModelId;
  onModelChange?: (value: ModelId) => void;
  effort?: EffortLevel;
  onEffortChange?: (value: EffortLevel) => void;
  onEmptySubmit?: () => void;
  templatePicker?: React.ReactNode;
  stashedText?: string;
  stashDepth?: number;
  onStash?: (text: string) => void;
  onUnstash?: () => string | undefined;
}

/**
 * Presentational shell + coordinator. It owns attachments and the submit/speech
 * lifecycles, but the `text` state lives inside {@link ComposerTextarea}; the
 * shell talks to it through an imperative handle. The toolbar and right-hand
 * actions are passed in as a stable `bottomBar` element, so a keystroke (which
 * only mutates the inner textarea) never re-renders this subtree.
 */
export const MessageComposer = forwardRef<ComposerHandle, MessageComposerProps>(
  function MessageComposer(
    {
      projectId,
      onSend,
      disabled,
      isRunning,
      placeholder,
      onInterrupt,
      initialText,
      onTextPersist,
      worktree,
      onWorktreeChange,
      planMode,
      onPlanModeChange,
      autoApproveMode,
      onAutoApproveModeChange,
      provider,
      onProviderChange,
      attachmentsSupported = true,
      model,
      onModelChange,
      effort,
      onEffortChange,
      onEmptySubmit,
      templatePicker,
      stashedText,
      stashDepth,
      onStash,
      onUnstash,
    },
    ref,
  ) {
    const {
      attachments,
      isDragging,
      lightboxSrc,
      setLightboxSrc,
      fileInputRef,
      removeAttachment,
      clearAll,
      handlePaste,
      handleFileInput,
      handleDrop,
      handleDragOver,
      handleDragLeave,
    } = useAttachments();

    const inputRef = useRef<ComposerTextareaHandle>(null);
    const [hasContent, setHasContent] = useState((initialText ?? "").trim().length > 0);

    // Stable bridges to the inner textarea's state.
    const getText = useCallback(() => inputRef.current?.getText() ?? "", []);
    const setText = useCallback((value: string) => inputRef.current?.setText(value), []);
    const clearComposer = useCallback(() => inputRef.current?.clear(), []);
    const handleContentChange = useCallback((value: boolean) => setHasContent(value), []);

    const speech = useComposerSpeech({ getText, setText });

    const send = useComposerSend({
      getText,
      setText,
      clearComposer,
      getAttachments: () => attachments,
      clearAttachments: clearAll,
      onSend,
      disabled,
      onBeforeSend: speech.forceStop,
    });

    useImperativeHandle(
      ref,
      () => ({
        setText: (value: string) => inputRef.current?.setText(value, { focus: true }),
      }),
      [],
    );

    // Enter-key behavior: empty-submit hook fires only with no text and no attachments.
    const handleEnter = useCallback(() => {
      if (!getText().trim() && attachments.length === 0 && onEmptySubmit) {
        onEmptySubmit();
        return;
      }
      void send.handleSend();
    }, [getText, attachments.length, onEmptySubmit, send.handleSend]);

    const onAttachClick = useCallback(() => fileInputRef.current?.click(), [fileInputRef]);

    const isSendDisabled =
      !!disabled || send.submitting || (!hasContent && attachments.length === 0);

    const stashBanner = stashedText ? (
      <button
        type="button"
        onClick={() => {
          const restored = onUnstash?.();
          if (restored) inputRef.current?.setText(restored, { focus: true });
        }}
        className="flex items-center gap-1.5 mx-3 mt-2 px-2 py-1 rounded-md bg-primary/10 text-primary text-xs hover:bg-primary/20 transition-colors cursor-pointer group"
        title="Click to restore stashed text"
      >
        <ClipboardPaste className="h-3 w-3 shrink-0" />
        <span className="truncate max-w-[300px]">{stashedText}</span>
        {(stashDepth ?? 0) > 1 && (
          <span className="text-primary/70 shrink-0 font-medium">({stashDepth})</span>
        )}
        <span className="text-primary/50 group-hover:text-primary/70 shrink-0">⌃S restore</span>
      </button>
    ) : undefined;

    const bottomBar = (
      <div className="flex items-center justify-between px-2 pb-2">
        <ComposerToolbar
          attachmentsSupported={attachmentsSupported}
          onAttachClick={onAttachClick}
          disabled={!!disabled || send.submitting}
          templatePicker={templatePicker}
          worktree={worktree}
          onWorktreeChange={onWorktreeChange}
          planMode={planMode}
          onPlanModeChange={onPlanModeChange}
          isRunning={isRunning}
          autoApproveMode={autoApproveMode}
          onAutoApproveModeChange={onAutoApproveModeChange}
          provider={provider}
          onProviderChange={onProviderChange}
          model={model}
          onModelChange={onModelChange}
          effort={effort}
          onEffortChange={onEffortChange}
        />

        <div className="flex items-center gap-1">
          {speech.isSupported && (
            <button
              type="button"
              {...speech.micHandlers}
              disabled={!!disabled || send.submitting}
              className={cn(
                "h-8 w-8 max-md:h-10 max-md:w-10 rounded-lg flex items-center justify-center transition-colors cursor-pointer disabled:opacity-40 disabled:cursor-not-allowed select-none touch-manipulation",
                speech.isListening
                  ? "text-destructive bg-destructive/10 mic-pulse"
                  : "text-muted-foreground hover:text-foreground hover:bg-muted/80",
              )}
              aria-label={speech.isListening ? "Stop dictation" : "Start dictation"}
              title="Click to toggle, hold to dictate (Ctrl+Shift+M)"
            >
              {speech.isListening ? (
                <MicOff className="h-3.5 w-3.5" />
              ) : (
                <Mic className="h-3.5 w-3.5" />
              )}
            </button>
          )}
          {isRunning && (
            <button
              type="button"
              onClick={onInterrupt}
              className="h-8 w-8 max-md:h-10 max-md:w-10 rounded-lg text-destructive hover:bg-destructive/10 flex items-center justify-center transition-colors cursor-pointer"
              aria-label="Stop"
            >
              <Square className="h-3.5 w-3.5" />
            </button>
          )}
          <button
            type="button"
            onClick={() => void send.handleSend()}
            disabled={isSendDisabled}
            className="h-8 w-8 max-md:h-10 max-md:w-10 rounded-lg bg-agent text-background flex items-center justify-center transition-colors hover:bg-agent/90 disabled:opacity-30 disabled:cursor-not-allowed cursor-pointer"
            aria-label={isRunning ? "Queue message" : "Send message"}
          >
            {isRunning ? (
              <ListPlus className="h-3.5 w-3.5" />
            ) : (
              <SendHorizonal className="h-3.5 w-3.5" />
            )}
          </button>
        </div>
      </div>
    );

    return (
      <div className="p-3 pb-[max(0.75rem,env(safe-area-inset-bottom))] shrink-0">
        <AttachmentStrip
          attachments={attachments}
          onRemove={removeAttachment}
          onPreview={setLightboxSrc}
        />

        <ComposerTextarea
          ref={inputRef}
          projectId={projectId}
          initialText={initialText}
          placeholder={placeholder ?? (isRunning ? "Queue a follow-up..." : "Send a message...")}
          disabled={!!disabled || send.submitting}
          busy={send.submitting}
          isDragging={isDragging}
          dropHandlers={{
            onDrop: handleDrop,
            onDragOver: handleDragOver,
            onDragLeave: handleDragLeave,
          }}
          onPaste={handlePaste}
          stashBanner={stashBanner}
          bottomBar={bottomBar}
          onContentChange={handleContentChange}
          onSubmit={handleEnter}
          onStash={onStash}
          onUnstash={onUnstash}
          onToggleSpeech={speech.isSupported ? speech.toggle : undefined}
          speechSupported={speech.isSupported}
          onTextPersist={onTextPersist}
        />

        <input
          ref={fileInputRef}
          type="file"
          accept={ACCEPTED_TYPES}
          multiple
          className="hidden"
          onChange={handleFileInput}
        />

        <ImageLightbox src={lightboxSrc} onClose={() => setLightboxSrc(null)} />
      </div>
    );
  },
);
