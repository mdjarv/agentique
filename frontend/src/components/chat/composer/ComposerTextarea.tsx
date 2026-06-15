import type { ClipboardEvent, DragEvent, ReactNode } from "react";
import { forwardRef, useCallback, useEffect, useImperativeHandle, useRef, useState } from "react";
import { useAutocomplete } from "~/hooks/useAutocomplete";
import { useAutosizeTextarea } from "~/hooks/useAutosizeTextarea";
import { useIsMobile } from "~/hooks/useIsMobile";
import { cn } from "~/lib/utils";
import { AutocompletePopup } from "../AutocompletePopup";

export interface ComposerTextareaHandle {
  getText: () => string;
  setText: (text: string, opts?: { focus?: boolean }) => void;
  /** Empties the field and closes the autocomplete popup. */
  clear: () => void;
  focus: () => void;
}

interface ComposerTextareaProps {
  projectId: string;
  initialText?: string;
  placeholder: string;
  /** disabled || submitting */
  disabled: boolean;
  /** submitting — drives aria-busy */
  busy: boolean;
  isDragging: boolean;
  dropHandlers: {
    onDrop: (e: DragEvent) => void;
    onDragOver: (e: DragEvent) => void;
    onDragLeave: (e: DragEvent) => void;
  };
  onPaste: (e: ClipboardEvent) => void;
  /** Banner rendered above the textarea (stash affordance). */
  stashBanner?: ReactNode;
  /** Bottom bar rendered below the textarea. Pass a stable element so typing skips it. */
  bottomBar: ReactNode;
  /** Fires only when trimmed-emptiness flips, so the shell re-renders on edges, not per keystroke. */
  onContentChange: (hasContent: boolean) => void;
  /** Enter-key behavior (empty-submit-or-send), decided by the shell which knows attachments. */
  onSubmit: () => void;
  onStash?: (text: string) => void;
  onUnstash?: () => string | undefined;
  onToggleSpeech?: () => void;
  speechSupported: boolean;
  onTextPersist?: (text: string) => void;
}

/**
 * Owns the composer's `text` state, autocomplete, autosize, and draft persistence.
 * Everything text-dependent lives here so a keystroke re-renders only this subtree;
 * the toolbar/right-actions arrive as a stable `bottomBar` element and are skipped.
 * The shell drives programmatic edits (speech, send, stash, external setText)
 * through the imperative handle.
 */
export const ComposerTextarea = forwardRef<ComposerTextareaHandle, ComposerTextareaProps>(
  function ComposerTextarea(
    {
      projectId,
      initialText,
      placeholder,
      disabled,
      busy,
      isDragging,
      dropHandlers,
      onPaste,
      stashBanner,
      bottomBar,
      onContentChange,
      onSubmit,
      onStash,
      onUnstash,
      onToggleSpeech,
      speechSupported,
      onTextPersist,
    },
    ref,
  ) {
    const isMobile = useIsMobile();
    const [text, setTextState] = useState(initialText ?? "");
    const textareaRef = useRef<HTMLTextAreaElement>(null);
    const textRef = useRef(text);
    textRef.current = text;

    const setText = useCallback((value: string) => {
      textRef.current = value;
      setTextState(value);
    }, []);

    useAutosizeTextarea(textareaRef, text);

    const autocomplete = useAutocomplete({ projectId, textareaRef, text, onTextChange: setText });

    // Notify the shell only on the empty↔non-empty edge (setState bails on equal values).
    const onContentChangeRef = useRef(onContentChange);
    onContentChangeRef.current = onContentChange;
    useEffect(() => {
      onContentChangeRef.current(text.trim().length > 0);
    }, [text]);

    // Draft persistence — debounced while typing, and once more on unmount.
    const onTextPersistRef = useRef(onTextPersist);
    onTextPersistRef.current = onTextPersist;
    useEffect(() => {
      const timer = setTimeout(() => onTextPersistRef.current?.(text), 500);
      return () => clearTimeout(timer);
    }, [text]);
    useEffect(() => {
      return () => {
        onTextPersistRef.current?.(textRef.current);
      };
    }, []);

    // Place the caret at the end of any prefilled text on mount.
    useEffect(() => {
      const el = textareaRef.current;
      if (el?.value) {
        el.selectionStart = el.selectionEnd = el.value.length;
      }
    }, []);

    const focusEnd = useCallback(() => {
      requestAnimationFrame(() => textareaRef.current?.focus());
    }, []);

    useImperativeHandle(
      ref,
      () => ({
        getText: () => textRef.current,
        setText: (value: string, opts?: { focus?: boolean }) => {
          setText(value);
          if (opts?.focus) focusEnd();
        },
        clear: () => {
          setText("");
          autocomplete.close();
        },
        focus: () => textareaRef.current?.focus(),
      }),
      [setText, focusEnd, autocomplete.close],
    );

    const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
      // Ctrl/Cmd+S → stash current input, or pop stash if input is empty.
      if (e.key === "s" && (e.ctrlKey || e.metaKey) && !e.shiftKey) {
        e.preventDefault();
        const trimmed = text.trim();
        if (trimmed && onStash) {
          onStash(trimmed);
          setText("");
        } else if (!trimmed && onUnstash) {
          const restored = onUnstash();
          if (restored) {
            setText(restored);
            focusEnd();
          }
        }
        return;
      }
      // Ctrl/Cmd+Shift+M → toggle dictation.
      if (e.key === "M" && e.shiftKey && (e.ctrlKey || e.metaKey) && speechSupported) {
        e.preventDefault();
        onToggleSpeech?.();
        return;
      }
      autocomplete.onKeyDown(e);
      if (e.defaultPrevented) return;
      if (e.key === "Enter" && !e.shiftKey && !isMobile && !e.nativeEvent.isComposing) {
        e.preventDefault();
        onSubmit();
      }
    };

    return (
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
          onDrop={dropHandlers.onDrop}
          onDragOver={dropHandlers.onDragOver}
          onDragLeave={dropHandlers.onDragLeave}
        >
          {stashBanner}
          <textarea
            ref={textareaRef}
            autoFocus
            value={text}
            onChange={(e) => setText(e.target.value)}
            onKeyDown={handleKeyDown}
            onPaste={onPaste}
            placeholder={placeholder}
            enterKeyHint={isMobile ? "enter" : "send"}
            className="w-full resize-none bg-transparent px-3 pt-3 pb-1 text-sm placeholder:text-muted-foreground focus:outline-none overflow-y-auto"
            rows={1}
            style={{ maxHeight: "200px" }}
            disabled={disabled}
            aria-busy={busy}
          />
          {bottomBar}
        </div>
      </div>
    );
  },
);
