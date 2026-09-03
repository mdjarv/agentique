import {
  ClipboardPaste,
  ListPlus,
  Mic,
  MicOff,
  Phone,
  Plus,
  SendHorizonal,
  Square,
} from "lucide-react";
import { forwardRef, useCallback, useImperativeHandle, useRef, useState } from "react";
import { useAttachments } from "~/hooks/useAttachments";
import { useIsMobile } from "~/hooks/useIsMobile";
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
  /** Current text — for callers that must not clobber what is being typed. */
  getText: () => string;
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
  /** Session-specific label, including the concrete version when reported. */
  modelDisplayName?: string;
  onModelChange?: (value: ModelId) => void;
  effort?: EffortLevel;
  onEffortChange?: (value: EffortLevel) => void;
  onEmptySubmit?: () => void;
  templatePicker?: React.ReactNode;
  /**
   * Mobile "focus" layout: one flush row — a `+` tools tray, the field, and
   * send — with everything else behind the tray. No-op on desktop and on the
   * new-session form (opt-in).
   */
  focusMode?: boolean;
  /**
   * The context meter, drawn as the shell's top edge in the focus layout. A
   * node rather than the usage itself: the composer is about the next message
   * and has no business reading a session's token counts.
   */
  topEdge?: React.ReactNode;
  /** Focus entered or left the composer. See `ComposerTextarea`. */
  onFocusWithinChange?: (focused: boolean) => void;
  /**
   * Opens a live voice call for this session. Absent hides the control, which
   * is what an unconfigured [voice] backend looks like from here.
   */
  onStartLive?: () => void;
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
      autoApproveMode,
      onAutoApproveModeChange,
      provider,
      onProviderChange,
      attachmentsSupported = true,
      model,
      modelDisplayName,
      onModelChange,
      effort,
      onEffortChange,
      onEmptySubmit,
      templatePicker,
      focusMode,
      topEdge,
      onFocusWithinChange,
      onStartLive,
      stashedText,
      stashDepth,
      onStash,
      onUnstash,
    },
    ref,
  ) {
    const isMobile = useIsMobile();
    const [showTools, setShowTools] = useState(false);
    // Focus layout only kicks in on a narrow viewport; desktop keeps the inline toolbar.
    const useFocusLayout = !!focusMode && isMobile;
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
        getText: () => inputRef.current?.getText() ?? "",
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

    const toolbar = (
      <ComposerToolbar
        attachmentsSupported={attachmentsSupported}
        onAttachClick={onAttachClick}
        disabled={!!disabled || send.submitting}
        templatePicker={templatePicker}
        isRunning={isRunning}
        autoApproveMode={autoApproveMode}
        onAutoApproveModeChange={onAutoApproveModeChange}
        provider={provider}
        onProviderChange={onProviderChange}
        model={model}
        modelDisplayName={modelDisplayName}
        onModelChange={onModelChange}
        effort={effort}
        onEffortChange={onEffortChange}
      />
    );

    // The phone drops dictation: the keyboard two rows below the composer has
    // its own mic, and duplicating a platform control cost a 40px target on the
    // one layout that has none to spare. Ctrl+Shift+M still works where there
    // is a keyboard to press it on.
    const showMic = speech.isSupported && !useFocusLayout;
    // A phone icon beside a half-written message is a mis-tap in front of Send,
    // so the call steps aside once there is something to send. It comes back
    // when the field is empty, which is when a call is what you wanted anyway.
    const showLive = !!onStartLive && !(useFocusLayout && hasContent);

    const rightActions = (
      <div className="flex items-center gap-1">
        {showMic && (
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
        {showLive && (
          <button
            type="button"
            onClick={onStartLive}
            disabled={!!disabled || send.submitting}
            className="h-8 w-8 max-md:h-10 max-md:w-10 rounded-lg flex items-center justify-center transition-colors cursor-pointer text-muted-foreground hover:text-agent hover:bg-muted/80 disabled:opacity-40 disabled:cursor-not-allowed"
            aria-label="Start a live voice call"
            title="Live — talk it through, it drafts and runs the prompt"
          >
            <Phone className="h-3.5 w-3.5" />
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
    );

    // The `+` toggle: the only thing left of the field in the focus layout.
    // Attach, templates, the brain and the permission mode are all behind it —
    // the tray is where a setting is *changed*; where it is *read* is the
    // header's subline, which is idle exactly when the reading is worth having.
    const trayToggle = (
      <button
        type="button"
        onClick={() => setShowTools((v) => !v)}
        className={cn(
          "h-9 w-9 rounded-lg flex items-center justify-center transition-colors cursor-pointer shrink-0",
          showTools
            ? "bg-agent/15 text-agent"
            : "text-muted-foreground hover:text-foreground hover:bg-muted/80",
        )}
        aria-label={showTools ? "Hide tools" : "Show tools"}
        aria-expanded={showTools}
      >
        <Plus className={cn("h-5 w-5 transition-transform", showTools && "rotate-45")} />
      </button>
    );

    const tray =
      useFocusLayout && showTools ? (
        <div className="border-b border-border/50 bg-muted/20 px-1.5 py-1">{toolbar}</div>
      ) : undefined;

    const bottomBar = useFocusLayout ? null : (
      <div className="flex items-center justify-between px-2 pb-2">
        {toolbar}
        {rightActions}
      </div>
    );

    const textarea = (
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
        inline={useFocusLayout}
        topEdge={useFocusLayout ? topEdge : undefined}
        tray={tray}
        leading={useFocusLayout ? trayToggle : undefined}
        trailing={useFocusLayout ? rightActions : undefined}
        onFocusWithinChange={onFocusWithinChange}
        onContentChange={handleContentChange}
        onSubmit={handleEnter}
        onStash={onStash}
        onUnstash={onUnstash}
        onToggleSpeech={speech.isSupported ? speech.toggle : undefined}
        speechSupported={speech.isSupported}
        onTextPersist={onTextPersist}
      />
    );

    const fileField = (
      <input
        ref={fileInputRef}
        type="file"
        accept={ACCEPTED_TYPES}
        multiple
        className="hidden"
        onChange={handleFileInput}
      />
    );

    // Flush on the phone — no card, no outer padding, only the bottom inset the
    // hardware demands.
    if (useFocusLayout) {
      return (
        <div className="shrink-0">
          <div className="px-2">
            <AttachmentStrip
              attachments={attachments}
              onRemove={removeAttachment}
              onPreview={setLightboxSrc}
            />
          </div>
          {textarea}
          <div className="h-[env(safe-area-inset-bottom)]" />
          {fileField}
          <ImageLightbox src={lightboxSrc} onClose={() => setLightboxSrc(null)} />
        </div>
      );
    }

    return (
      <div className="p-3 pb-[max(0.75rem,env(safe-area-inset-bottom))] shrink-0">
        <AttachmentStrip
          attachments={attachments}
          onRemove={removeAttachment}
          onPreview={setLightboxSrc}
        />
        {textarea}
        {fileField}
        <ImageLightbox src={lightboxSrc} onClose={() => setLightboxSrc(null)} />
      </div>
    );
  },
);
