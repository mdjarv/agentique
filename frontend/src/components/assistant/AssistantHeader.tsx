/**
 * The assistant's band: its orb, a name, and whatever that page does.
 *
 * Two pages wear it — the thread (`/assistant`) and memory
 * (`/assistant/memory`) — so it is one component rather than two headers that
 * drift. The orb is the same mark the rail row wears, and during a call it
 * takes the call's state, so no surface of the assistant can disagree with
 * another about whether a line is open.
 *
 * Controls are the caller's: `AssistantThreadHeader` carries the thread's three
 * (the digest, memory, and the call), and the memory page passes its own. The shell places
 * the orb and the name and nothing else.
 */
import { Link } from "@tanstack/react-router";
import { Brain, Newspaper, Phone } from "lucide-react";
import { type ReactNode, useState } from "react";
import { toast } from "sonner";
import { PageHeader } from "~/components/layout/PageHeader";
import { HaloOrb } from "~/components/voice/HaloOrb";
import { useCallView } from "~/components/voice/use-call-view";
import { useWebSocket } from "~/hooks/useWebSocket";
import { digest } from "~/lib/assistant/rpc";
import { getErrorMessage } from "~/lib/utils";
import { useFeatureStore } from "~/stores/feature-store";
import { useVoiceStore } from "~/stores/voice-store";

/** Shared by every control in this band, so they read as one row of marks. */
const CONTROL_CLASS =
  "h-8 w-8 rounded-lg flex items-center justify-center transition-colors cursor-pointer text-muted-foreground hover:text-agent hover:bg-muted/80";

export function AssistantHeader({ title, children }: { title: string; children?: ReactNode }) {
  const view = useCallView();
  return (
    <PageHeader>
      <HaloOrb size={20} state={view.active ? view.orbState : "idle"} glyph="none" />
      <span className="font-medium truncate">{title}</span>
      {children}
    </PageHeader>
  );
}

/**
 * The thread's header, and the three things you can do to the assistant from it.
 *
 * **Memory** is where what it remembers lives — one home, under the assistant,
 * which is why the rail's ⋯ menu no longer lists it. It is drawn only when the
 * server mounted the brain (`features.brain`), because an unmounted `/api/`
 * path answers the SPA rather than a 404 and the page would look alive.
 *
 * **Digest** asks for what has happened since the last one, now. It is the
 * notifier's deterministic summary — the journal grouped by the needs-you
 * ranking — and it lands as a message in the conversation, so the control has
 * nothing to render: it fires the op and the ordinary `assistant.message` push
 * delivers the result. Ungated, because the digest is the core's and not the
 * brain's or voice's.
 *
 * **The phone** places an UNFOCUSED call: it means "talk to this instead of
 * typing", where a session composer's phone means "talk about this session".
 * It steps aside while a call exists, because the call's own surfaces carry its
 * controls and a second phone would read as a second line.
 */
export function AssistantThreadHeader() {
  const voiceEnabled = useFeatureStore((s) => s.features.voice);
  const brainEnabled = useFeatureStore((s) => s.features.brain);
  const start = useVoiceStore((s) => s.start);
  const view = useCallView();
  return (
    <AssistantHeader title="Assistant">
      <div className="ml-auto flex shrink-0 items-center gap-0.5">
        <DigestControl />
        {brainEnabled && (
          <Link to="/assistant/memory" aria-label="Memory" title="Memory" className={CONTROL_CLASS}>
            <Brain className="h-3.5 w-3.5" />
          </Link>
        )}
        {voiceEnabled && !view.active && (
          <button
            type="button"
            onClick={() => start()}
            aria-label="Start a live call with the assistant"
            title="Talk instead (⌥V)"
            className={CONTROL_CLASS}
          >
            <Phone className="h-3.5 w-3.5" />
          </button>
        )}
      </div>
    </AssistantHeader>
  );
}

/**
 * The Digest control.
 *
 * A newspaper, because that is what it hands over: everything since the last
 * one, in one read, whether or not anything needed a decision. It is disabled
 * while one is composing rather than being pressable twice — a second digest
 * would stamp the window and report an empty one.
 *
 * Nothing is rendered from the answer. The digest is a stored message and
 * arrives on `assistant.message` like any other, so the conversation below is
 * where it appears; a failure is a toast, because the press had no other
 * visible effect to contradict.
 */
function DigestControl() {
  const ws = useWebSocket();
  const [pending, setPending] = useState(false);
  return (
    <button
      type="button"
      disabled={pending}
      onClick={() => {
        setPending(true);
        digest(ws)
          .catch((err) => toast.error(getErrorMessage(err, "The assistant could not digest that")))
          .finally(() => setPending(false));
      }}
      aria-label="Digest"
      title="What has happened since the last digest"
      className={CONTROL_CLASS}
    >
      <Newspaper className="h-3.5 w-3.5" />
    </button>
  );
}
