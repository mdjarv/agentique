import {
  FileText,
  FolderOpen,
  Gauge,
  GitBranch,
  ListChecks,
  ListPlus,
  MessageSquare,
  Mic,
  MicOff,
  Paperclip,
  SendHorizonal,
  ShieldAlert,
  ShieldCheck,
  Square,
  X,
} from "lucide-react";
import { forwardRef, useCallback, useEffect, useImperativeHandle, useRef, useState } from "react";
import { useAttachments } from "~/hooks/useAttachments";
import { useAutocomplete } from "~/hooks/useAutocomplete";
import { useIsMobile } from "~/hooks/useIsMobile";
import { useSpeechRecognition } from "~/hooks/useSpeechRecognition";
import {
  ACCEPTED_TYPES,
  EFFORT_COLORS,
  EFFORT_LABELS,
  EFFORT_LEVELS,
  type EffortLevel,
  isImage,
  PERMISSION_BG,
  PERMISSION_COLORS,
  PERMISSION_DESCRIPTIONS,
  PERMISSION_LABELS,
  PERMISSION_MODES,
} from "~/lib/composer-constants";
import { MODEL_LABELS, MODELS, type ModelId } from "~/lib/session/actions";
import { cn } from "~/lib/utils";
import type { Attachment, AutoApproveMode } from "~/stores/chat-store";
import { AutocompletePopup } from "./AutocompletePopup";
import { ImageLightbox } from "./ImageLightbox";
import { ToolbarDropdown, type ToolbarDropdownOption } from "./ToolbarDropdown";
import { ToolbarToggle } from "./ToolbarToggle";

export type { EffortLevel };

export interface ComposerHandle {
  setText: (text: string) => void;
}

interface MessageComposerProps {
  projectId: string;
  onSend: (prompt: string, attachments?: Attachment[]) => void;
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
  model?: ModelId;
  onModelChange?: (value: ModelId) => void;
  effort?: EffortLevel;
  onEffortChange?: (value: EffortLevel) => void;
  onEmptySubmit?: () => void;
  templatePicker?: React.ReactNode;
}

const PERMISSION_OPTIONS: ToolbarDropdownOption[] = PERMISSION_MODES.map((m) => ({
  value: m,
  label: PERMISSION_LABELS[m],
  icon:
    m === "fullAuto" ? <ShieldAlert className="h-3 w-3" /> : <ShieldCheck className="h-3 w-3" />,
  color: PERMISSION_COLORS[m],
  description: PERMISSION_DESCRIPTIONS[m],
}));

const MODEL_OPTIONS: ToolbarDropdownOption[] = MODELS.map((m) => ({
  value: m,
  label: MODEL_LABELS[m],
}));

const EFFORT_OPTIONS: ToolbarDropdownOption[] = EFFORT_LEVELS.map((lvl) => ({
  value: lvl,
  label: EFFORT_LABELS[lvl],
  color: EFFORT_COLORS[lvl],
}));

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
      model,
      onModelChange,
      effort,
      onEffortChange,
      onEmptySubmit,
      templatePicker,
    },
    ref,
  ) {
    const isMobile = useIsMobile();
    const [text, setText] = useState(initialText ?? "");
    const textareaRef = useRef<HTMLTextAreaElement>(null);
    const submittingRef = useRef(false);
    const textRef = useRef(text);
    textRef.current = text;
    const onTextPersistRef = useRef(onTextPersist);
    onTextPersistRef.current = onTextPersist;

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

    const autocomplete = useAutocomplete({ projectId, textareaRef, text, onTextChange: setText });

    // --- Speech recognition (dictation mode) ---
    // speechBaseRef: text that existed when the current recognition session started.
    // Everything after this base is replaced by the latest transcript on each update.
    const speechBaseRef = useRef("");

    const speech = useSpeechRecognition({
      onBeforeStart: useCallback(() => {
        speechBaseRef.current = textRef.current;
      }, []),
      onTranscript: useCallback((transcript: string) => {
        const base = speechBaseRef.current;
        const spacer = base && !base.endsWith(" ") && !base.endsWith("\n") ? " " : "";
        setText(base + spacer + transcript);
        requestAnimationFrame(() => {
          const el = textareaRef.current;
          if (el) {
            el.style.height = "auto";
            el.style.height = `${Math.min(el.scrollHeight, 200)}px`;
          }
        });
      }, []),
    });

    // Press-and-hold: hold >500ms starts "hold mode" — release stops.
    // Short click (<500ms) toggles.
    // If already listening on pointerDown, force-stop immediately (escape hatch).
    const holdTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const holdActiveRef = useRef(false);
    const didForceStopRef = useRef(false);

    const handleMicPointerDown = useCallback(
      (e: React.PointerEvent) => {
        if (e.button !== 0) return;
        try {
          (e.target as HTMLElement).setPointerCapture(e.pointerId);
        } catch {
          // Pointer capture not supported or invalid pointer — continue without it.
        }

        // Clear any leftover hold timer (e.g. if previous pointerUp was missed).
        if (holdTimerRef.current) {
          clearTimeout(holdTimerRef.current);
          holdTimerRef.current = null;
        }
        didForceStopRef.current = false;

        // Escape hatch: if UI shows listening, force-stop unconditionally.
        if (speech.isListening) {
          speech.forceStop();
          didForceStopRef.current = true;
          return;
        }

        holdActiveRef.current = false;
        holdTimerRef.current = setTimeout(() => {
          holdTimerRef.current = null;
          holdActiveRef.current = true;
          speech.start();
        }, 500);
      },
      [speech],
    );

    const handleMicPointerUp = useCallback(() => {
      if (holdTimerRef.current) {
        clearTimeout(holdTimerRef.current);
        holdTimerRef.current = null;
      }
      if (didForceStopRef.current) {
        didForceStopRef.current = false;
        return;
      }
      if (holdActiveRef.current) {
        holdActiveRef.current = false;
        speech.stop();
      } else {
        speech.toggle();
      }
    }, [speech]);

    // Clean up hold timer if component unmounts mid-press.
    useEffect(() => {
      return () => {
        if (holdTimerRef.current) clearTimeout(holdTimerRef.current);
      };
    }, []);

    useImperativeHandle(ref, () => ({
      setText: (value: string) => {
        setText(value);
        requestAnimationFrame(() => {
          const el = textareaRef.current;
          if (el) {
            el.style.height = "auto";
            el.style.height = `${Math.min(el.scrollHeight, 200)}px`;
            el.focus();
          }
        });
      },
    }));

    useEffect(() => {
      const el = textareaRef.current;
      if (el?.value) {
        el.style.height = "auto";
        el.style.height = `${Math.min(el.scrollHeight, 200)}px`;
        el.selectionStart = el.selectionEnd = el.value.length;
      }
      return () => {
        onTextPersistRef.current?.(textRef.current);
      };
    }, []);

    useEffect(() => {
      const timer = setTimeout(() => {
        onTextPersistRef.current?.(text);
      }, 500);
      return () => clearTimeout(timer);
    }, [text]);

    const handleSend = () => {
      const trimmed = text.trim();
      if ((!trimmed && attachments.length === 0) || disabled || submittingRef.current) return;
      submittingRef.current = true;
      speech.forceStop();
      autocomplete.close();
      onSend(trimmed, attachments.length > 0 ? attachments : undefined);
      setText("");
      clearAll();
      if (textareaRef.current) {
        textareaRef.current.style.height = "auto";
      }
      queueMicrotask(() => {
        submittingRef.current = false;
      });
    };

    const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
      // Ctrl/Cmd+Shift+M → toggle dictation
      if (e.key === "M" && e.shiftKey && (e.ctrlKey || e.metaKey) && speech.isSupported) {
        e.preventDefault();
        speech.toggle();
        return;
      }
      autocomplete.onKeyDown(e);
      if (e.defaultPrevented) return;
      if (e.key === "Enter" && !e.shiftKey && !isMobile && !e.nativeEvent.isComposing) {
        e.preventDefault();
        if (!text.trim() && attachments.length === 0 && onEmptySubmit) {
          onEmptySubmit();
          return;
        }
        handleSend();
      }
    };

    const handleInput = () => {
      const el = textareaRef.current;
      if (el) {
        el.style.height = "auto";
        el.style.height = `${Math.min(el.scrollHeight, 200)}px`;
      }
    };

    const hasToggles = worktree !== undefined || onPlanModeChange || autoApproveMode !== undefined;
    const mode = autoApproveMode ?? "manual";

    return (
      <div className="p-3 pb-[max(0.75rem,env(safe-area-inset-bottom))] shrink-0">
        {attachments.length > 0 && (
          <div className="flex gap-2 flex-wrap mb-2">
            {attachments.map((a) => (
              <div key={a.id} className="relative group">
                {isImage(a.mimeType) ? (
                  <button
                    type="button"
                    className="p-0 border-none bg-transparent cursor-pointer"
                    onClick={() => setLightboxSrc(a.previewUrl ?? a.dataUrl)}
                  >
                    <img
                      src={a.previewUrl ?? a.dataUrl}
                      alt={a.name}
                      className="h-16 w-16 object-cover rounded-md border"
                    />
                  </button>
                ) : (
                  <div className="h-16 w-16 rounded-md border bg-muted flex flex-col items-center justify-center gap-1 px-1">
                    <FileText className="h-5 w-5 text-muted-foreground" />
                    <span className="text-[9px] text-muted-foreground truncate w-full text-center">
                      {a.name}
                    </span>
                  </div>
                )}
                <button
                  type="button"
                  onClick={() => removeAttachment(a.id)}
                  className="absolute -top-1.5 -right-1.5 h-4 w-4 rounded-full bg-destructive text-destructive-foreground flex items-center justify-center max-md:opacity-100 opacity-0 group-hover:opacity-100 transition-opacity"
                >
                  <X className="h-2.5 w-2.5" />
                </button>
              </div>
            ))}
          </div>
        )}

        <div className="relative">
          {autocomplete.isOpen && autocomplete.triggerType && (
            <AutocompletePopup
              items={autocomplete.items}
              selectedIndex={autocomplete.selectedIndex}
              triggerType={autocomplete.triggerType}
              onSelect={autocomplete.accept}
            />
          )}
          <div
            className={cn(
              "rounded-xl border bg-agent/5 transition-all",
              isDragging
                ? "border-agent ring-2 ring-agent/30"
                : "focus-within:border-agent/50 focus-within:ring-1 focus-within:ring-agent/30",
            )}
            onDrop={handleDrop}
            onDragOver={handleDragOver}
            onDragLeave={handleDragLeave}
          >
            <textarea
              ref={textareaRef}
              autoFocus
              value={text}
              onChange={(e) => {
                setText(e.target.value);
                handleInput();
              }}
              onKeyDown={handleKeyDown}
              onPaste={handlePaste}
              placeholder={
                placeholder ?? (isRunning ? "Queue a follow-up..." : "Send a message...")
              }
              enterKeyHint={isMobile ? "enter" : "send"}
              className="w-full resize-none bg-transparent px-3 pt-3 pb-1 text-sm placeholder:text-muted-foreground focus:outline-none overflow-y-auto"
              rows={1}
              style={{ maxHeight: "200px" }}
              disabled={disabled}
            />

            {/* Bottom bar */}
            <div className="flex items-center justify-between px-2 pb-2">
              <div className="flex items-center gap-0.5 max-md:gap-1 max-md:overflow-x-auto max-md:flex-nowrap min-w-0">
                <button
                  type="button"
                  onClick={() => fileInputRef.current?.click()}
                  disabled={disabled}
                  className="h-7 w-7 max-md:h-10 max-md:w-10 rounded-lg text-muted-foreground hover:text-foreground hover:bg-muted/80 flex items-center justify-center transition-colors disabled:opacity-40 cursor-pointer"
                  aria-label="Attach files"
                >
                  <Paperclip className="h-3.5 w-3.5" />
                </button>
                {templatePicker}

                {hasToggles && <div className="w-px h-4 bg-border mx-1 shrink-0" />}

                {worktree !== undefined && (
                  <ToolbarToggle
                    active={worktree}
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

                {(effort !== undefined || model) && (
                  <div className="w-px h-4 bg-border mx-1 shrink-0" />
                )}

                {model && (
                  <ToolbarDropdown
                    value={model}
                    onChange={onModelChange ? (v) => onModelChange(v as ModelId) : undefined}
                    options={MODEL_OPTIONS}
                  />
                )}
                {effort !== undefined && (
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

              <div className="flex items-center gap-1">
                {speech.isSupported && (
                  <button
                    type="button"
                    onPointerDown={handleMicPointerDown}
                    onPointerUp={handleMicPointerUp}
                    onContextMenu={(e) => e.preventDefault()}
                    disabled={disabled}
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
                  onClick={handleSend}
                  disabled={disabled || (!text.trim() && attachments.length === 0)}
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
          </div>
        </div>

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
