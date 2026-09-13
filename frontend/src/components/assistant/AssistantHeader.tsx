/**
 * The assistant's band: its orb, a name, and whatever that page does.
 *
 * Three pages wear it — the thread (`/assistant`), memory
 * (`/assistant/memory`) and policies (`/assistant/policies`) — so it is one
 * component rather than three headers that drift. Every page under the assistant
 * gets it for that reason: the orb and the name are what say which thing you are
 * inside. The orb is the same mark the rail row wears, and during a call it
 * takes the call's state, so no surface of the assistant can disagree with
 * another about whether a line is open.
 *
 * Controls are the caller's: `AssistantThreadHeader` carries the thread's own
 * (the call, and one ⋯ menu holding the rest), and the memory page passes its
 * own. The shell places the orb and the name and nothing else.
 */
import { Link } from "@tanstack/react-router";
import { Brain, Ellipsis, Newspaper, Phone, Scale } from "lucide-react";
import { type ReactNode, useState } from "react";
import { toast } from "sonner";
import { PageHeader } from "~/components/layout/PageHeader";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "~/components/ui/dropdown-menu";
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
 * The thread's header: the orb, the name, the call button, and one ⋯ menu.
 *
 * The band held four controls in a row and the fourth (Policies) was what made
 * it obvious they were not peers. **The call is the band's**, because it is the
 * one thing here that is about *now* — press it and a line opens. Everything
 * else is a place to go or a thing to ask for, and three marks in a row spend
 * the header's width saying that badly: a glyph row reads as one set of equals,
 * where two of these navigate and one fires an op.
 *
 * So the menu, on the rail's own ⋯ precedent, holding:
 *
 * **Memory**, where what it remembers lives — one home, under the assistant,
 * which is why the rail's ⋯ menu no longer lists it. It is drawn only when the
 * server mounted the brain (`features.brain`), because an unmounted `/api/`
 * path answers the SPA rather than a 404 and the page would look alive.
 *
 * **Policies**, the standing instructions the heartbeat may act under.
 * Ungated with the assistant's own page: the route says in words when the
 * assistant is off, which is more use than a row that is not there.
 *
 * **Digest**, which asks for what has happened since the last one, now. It is
 * the notifier's deterministic summary — the journal grouped by the needs-you
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
  const start = useVoiceStore((s) => s.start);
  const view = useCallView();
  return (
    <AssistantHeader title="Assistant">
      <div className="ml-auto flex shrink-0 items-center gap-0.5">
        <AssistantMenu />
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
 * The assistant's ⋯ menu: the two pages under it, and the digest.
 *
 * One trigger, on the rail's precedent, and it is the whole of the band's
 * secondary width. The Digest sits in here rather than on the band even though
 * it is an action and not a place: it is asked for occasionally, it renders
 * nothing of its own, and a glyph in a row of glyphs could not say that the
 * other two navigate.
 *
 * The digest is disabled while one is composing rather than being pressable
 * twice — a second digest would stamp the window and report an empty one. Its
 * answer is not rendered here: the digest is a stored message and arrives on
 * `assistant.message` like any other, so the conversation below is where it
 * appears, and a failure is a toast because the press had no other visible
 * effect to contradict.
 */
function AssistantMenu() {
  const ws = useWebSocket();
  const brainEnabled = useFeatureStore((s) => s.features.brain);
  const [pending, setPending] = useState(false);
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button type="button" aria-label="Assistant menu" title="More" className={CONTROL_CLASS}>
          <Ellipsis className="h-4 w-4" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="min-w-44">
        {brainEnabled && (
          <DropdownMenuItem asChild className="gap-2 text-xs">
            <Link to="/assistant/memory">
              <Brain className="h-3.5 w-3.5" />
              Memory
            </Link>
          </DropdownMenuItem>
        )}
        <DropdownMenuItem asChild className="gap-2 text-xs">
          <Link to="/assistant/policies">
            <Scale className="h-3.5 w-3.5" />
            Policies
          </Link>
        </DropdownMenuItem>
        <DropdownMenuItem
          className="gap-2 text-xs"
          disabled={pending}
          // The menu stays open for the round trip rather than closing on a
          // press whose result lands in the conversation behind it: the item is
          // the only thing that can say the ask is away.
          onSelect={(event) => {
            event.preventDefault();
            setPending(true);
            digest(ws)
              .catch((err) =>
                toast.error(getErrorMessage(err, "The assistant could not digest that")),
              )
              .finally(() => setPending(false));
          }}
        >
          <Newspaper className="h-3.5 w-3.5" />
          Digest
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
