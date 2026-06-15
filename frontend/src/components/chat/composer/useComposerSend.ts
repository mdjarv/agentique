import { useCallback, useEffect, useRef, useState } from "react";
import type { Attachment } from "~/stores/chat-store";

type SendResult = boolean | undefined;

interface UseComposerSendParams {
  /** Reads the current composer text (synchronous, ref-backed). */
  getText: () => string;
  /** Restores text after a failed send. */
  setText: (text: string) => void;
  /** Empties the composer (text + open autocomplete). Height resets via the value-keyed effect. */
  clearComposer: () => void;
  getAttachments: () => Attachment[];
  clearAttachments: () => void;
  onSend: (prompt: string, attachments?: Attachment[]) => SendResult | Promise<SendResult>;
  disabled?: boolean;
  /** Runs synchronously before the text is cleared — e.g. stop dictation. */
  onBeforeSend?: () => void;
}

export interface ComposerSend {
  submitting: boolean;
  handleSend: () => Promise<void>;
}

/**
 * Owns the submit lifecycle: re-entrancy guard, in-flight `submitting` flag, and
 * the restore-on-failure race. The composer is cleared optimistically before the
 * await; on failure the draft is restored *only if the field is still empty*, so
 * a user who started typing again during the in-flight send doesn't get clobbered.
 */
export function useComposerSend({
  getText,
  setText,
  clearComposer,
  getAttachments,
  clearAttachments,
  onSend,
  disabled,
  onBeforeSend,
}: UseComposerSendParams): ComposerSend {
  const [submitting, setSubmitting] = useState(false);
  const submittingRef = useRef(false);
  const mountedRef = useRef(true);

  // Mirror mutable inputs into refs so handleSend stays referentially stable
  // (it's wired into the textarea keydown + the send button) yet never goes stale.
  const onSendRef = useRef(onSend);
  onSendRef.current = onSend;
  const getAttachmentsRef = useRef(getAttachments);
  getAttachmentsRef.current = getAttachments;
  const clearAttachmentsRef = useRef(clearAttachments);
  clearAttachmentsRef.current = clearAttachments;
  const disabledRef = useRef(disabled);
  disabledRef.current = disabled;
  const onBeforeSendRef = useRef(onBeforeSend);
  onBeforeSendRef.current = onBeforeSend;

  useEffect(() => {
    return () => {
      mountedRef.current = false;
    };
  }, []);

  const handleSend = useCallback(async () => {
    const submittedText = getText();
    const trimmed = submittedText.trim();
    const attachments = getAttachmentsRef.current();
    if ((!trimmed && attachments.length === 0) || disabledRef.current || submittingRef.current) {
      return;
    }
    submittingRef.current = true;
    setSubmitting(true);
    onBeforeSendRef.current?.();
    clearComposer();

    let shouldClear = false;
    try {
      const result = await onSendRef.current(
        trimmed,
        attachments.length > 0 ? attachments : undefined,
      );
      shouldClear = result !== false;
    } catch (err) {
      console.error("send failed", err);
    } finally {
      submittingRef.current = false;
      if (mountedRef.current) setSubmitting(false);
    }

    if (!mountedRef.current) return;
    if (!shouldClear) {
      // Restore only if the user hasn't started a fresh draft in the meantime.
      if (getText() === "") setText(submittedText);
      return;
    }

    clearAttachmentsRef.current();
  }, [getText, setText, clearComposer]);

  return { submitting, handleSend };
}
