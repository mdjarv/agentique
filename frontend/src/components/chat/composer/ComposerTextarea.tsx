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
  /**
   * One-row layout: the field and its controls share a line and the shell goes
   * flush to the pane's edges. `leading`/`trailing` are the controls; the
   * stacked layout's `bottomBar` is not rendered. Mobile only — see
   * `MessageComposer`.
   */
  inline?: boolean;
  /**
   * Rendered above everything, outside the padding, at the shell's top edge —
   * the context meter. Full-bleed by construction, so it is the border rather
   * than something sitting on it.
   */
  topEdge?: ReactNode;
  /** The collapsible tools tray, above the input row and below `topEdge`. */
  tray?: ReactNode;
  /** Inline layout: controls left of the field. */
  leading?: ReactNode;
  /** Inline layout: controls right of the field (send lives here). */
  trailing?: ReactNode;
  /**
   * Focus entered or left the whole composer — not just the textarea, so
   * tapping a button inside it does not read as leaving. Drives the header's
   * condensed state.
   */
  onFocusWithinChange?: (focused: boolean) => void;
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
      inline = false,
      topEdge,
      tray,
      leading,
      trailing,
      onFocusWithinChange,
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

    // focusin/focusout, so a tap on a button inside the shell is not a blur.
    // `relatedTarget` is null when focus leaves the document entirely, which
    // counts as leaving.
    const focusHandlers = onFocusWithinChange
      ? {
          onFocus: () => onFocusWithinChange(true),
          onBlur: (e: React.FocusEvent<HTMLDivElement>) => {
            if (!e.currentTarget.contains(e.relatedTarget as Node | null)) {
              onFocusWithinChange(false);
            }
          },
        }
      : {};

    const field = (
      <textarea
        ref={textareaRef}
        // Not on the phone. Mobile browsers give the field focus without
        // raising the keyboard, so an autofocus there condenses the header
        // (`SessionHeader.condensed`) for a session nobody is typing in yet —
        // and it is the wrong gesture anyway: arriving at a session is reading
        // it, not answering it.
        autoFocus={!isMobile}
        value={text}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={handleKeyDown}
        onPaste={onPaste}
        placeholder={placeholder}
        enterKeyHint={isMobile ? "enter" : "send"}
        className={cn(
          // `block`: a textarea is inline-block by default, so its parent opens
          // a line box and adds ~6px of leading under it. That is what put the
          // placeholder half a line above the `+` beside it.
          "block w-full resize-none bg-transparent text-sm placeholder:text-muted-foreground focus:outline-none overflow-y-auto",
          // 20px line + 10px either side = 40, the height of the controls it
          // sits between, so one line of text is centred against them and extra
          // lines grow downward from the same baseline.
          inline ? "px-1 py-2.5" : "px-3 pt-3 pb-1",
        )}
        rows={1}
        style={{ maxHeight: inline ? "140px" : "200px" }}
        disabled={disabled}
        aria-busy={busy}
      />
    );

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
            "bg-agent/5 transition-all",
            // Flush to the pane on the phone: the card's border and its 12px of
            // outer padding were 26px of a screen that has 427.
            inline ? "border-t" : "rounded-xl border",
            isDragging
              ? "border-agent ring-2 ring-agent/30"
              : inline
                ? "border-agent/25 focus-within:border-agent/50"
                : "focus-within:border-agent/50 focus-within:ring-1 focus-within:ring-agent/30",
          )}
          onDrop={dropHandlers.onDrop}
          onDragOver={dropHandlers.onDragOver}
          onDragLeave={dropHandlers.onDragLeave}
          {...focusHandlers}
        >
          {topEdge}
          {tray}
          {stashBanner}
          {inline ? (
            <div className="flex items-end gap-1.5 px-2 py-1">
              {leading}
              <div className="min-w-0 flex-1">{field}</div>
              {trailing}
            </div>
          ) : (
            <>
              {field}
              {bottomBar}
            </>
          )}
        </div>
      </div>
    );
  },
);
