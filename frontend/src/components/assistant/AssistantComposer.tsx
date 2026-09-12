import { SendHorizonal } from "lucide-react";
import { useCallback, useRef, useState } from "react";
import {
  ComposerTextarea,
  type ComposerTextareaHandle,
} from "~/components/chat/composer/ComposerTextarea";

/**
 * The thread's composer, which is the ordinary composer.
 *
 * `ComposerTextarea` is what a session's `MessageComposer` is built on, so
 * Enter sends, Shift+Enter breaks a line, the phone gets a newline key and a
 * Send button, and the field autosizes — all of it the same behaviour as
 * everywhere else, because none of it is this page's to re-decide.
 *
 * What it does NOT bring is the session apparatus around that field:
 * attachments, templates, the brain control, the permission mark. The thread
 * has no project and no worktree, so there is nothing for those to act on —
 * `projectId` is empty, which the autocomplete's own fetches already tolerate
 * (they fail and are swallowed, so `@file` and `/command` simply offer
 * nothing).
 */

interface AssistantComposerProps {
  /** True while the head owes a reply. The field and Send are both closed. */
  replying: boolean;
  /** Runs with the trimmed text; the field is cleared only if it resolves. */
  onSend: (text: string) => Promise<void>;
}

export function AssistantComposer({ replying, onSend }: AssistantComposerProps) {
  const composerRef = useRef<ComposerTextareaHandle>(null);
  const [hasContent, setHasContent] = useState(false);
  const [sending, setSending] = useState(false);

  const disabled = replying || sending;

  const submit = useCallback(() => {
    const text = composerRef.current?.getText().trim();
    if (!text || disabled) return;
    setSending(true);
    // Cleared optimistically: the text is in hand, and leaving it in the field
    // while the ask is away invites a second send of the same message. A
    // failure puts it back rather than losing it.
    composerRef.current?.clear();
    onSend(text)
      .catch(() => composerRef.current?.setText(text, { focus: true }))
      .finally(() => setSending(false));
  }, [disabled, onSend]);

  return (
    <div className="shrink-0 border-t p-3 max-md:p-0">
      <ComposerTextarea
        ref={composerRef}
        projectId=""
        placeholder={replying ? "The assistant is answering…" : "Ask the assistant…"}
        disabled={disabled}
        busy={sending}
        onPaste={noop}
        onContentChange={setHasContent}
        onSubmit={submit}
        speechSupported={false}
        bottomBar={
          <div className="flex items-center justify-end px-2 pb-2">
            <button
              type="button"
              onClick={submit}
              disabled={!hasContent || disabled}
              className="size-8 rounded-lg bg-primary text-primary-foreground flex items-center justify-center transition-colors hover:bg-primary/90 disabled:opacity-30 disabled:cursor-not-allowed cursor-pointer"
              aria-label="Send to the assistant"
            >
              <SendHorizonal className="size-3.5" />
            </button>
          </div>
        }
      />
    </div>
  );
}

/** The thread takes no attachments, so a paste is just a paste. */
function noop() {}
