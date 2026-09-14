import { type UIEvent, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import { AssistantComposer } from "~/components/assistant/AssistantComposer";
import { AssistantConversation } from "~/components/assistant/AssistantConversation";
import { AssistantThreadHeader } from "~/components/assistant/AssistantHeader";
import { AssistantPinnedProposals } from "~/components/assistant/AssistantPinnedProposals";
import { HaloOrb } from "~/components/voice/HaloOrb";
import { useWebSocket } from "~/hooks/useWebSocket";
import { history, journal, markSeen as markSeenRpc, say } from "~/lib/assistant/rpc";
import { buildTimeline } from "~/lib/assistant/timeline";
import type { AssistantProposal } from "~/lib/assistant/wire";
import { getErrorMessage } from "~/lib/utils";
import {
  selectAssistantError,
  selectAssistantJournal,
  selectAssistantLoaded,
  selectAssistantMessages,
  selectAssistantOpenProposals,
  selectAssistantProposals,
  selectAssistantReplying,
  selectAssistantStreaming,
  useAssistantStore,
} from "~/stores/assistant-store";
import { useFeatureStore } from "~/stores/feature-store";

/**
 * The thread — `/assistant`, the page the rail's assistant row opens.
 *
 * A TRANSPORT, not a head: it brings no model. It renders the shared
 * conversation with the journal's news and the proposal cards merged into it,
 * and forwards the operator's text to the core's own head. Everything that
 * decides anything — refusals, tiers, budgets — is in `internal/assistant`,
 * where the call hits the same rules.
 *
 * **One scroll.** The page is a header, one timeline and the composer. News and
 * cards used to be bands above the conversation, each scrolling on its own, and
 * the conversation got what was left (design round 2026-09-14). What must stay
 * in view whatever the scroll — an open proposal — is pinned above the composer
 * while its card is out of sight, the way a session pins an approval.
 *
 * Mobile renders this page, not a variant of it: the same column at every
 * width, with the composer and the pin flush to the edges on a phone the way a
 * session's are.
 */
export function AssistantPage() {
  const ws = useWebSocket();
  const messages = useAssistantStore(selectAssistantMessages);
  const entries = useAssistantStore(selectAssistantJournal);
  const proposals = useAssistantStore(selectAssistantProposals);
  const openProposals = useAssistantStore(selectAssistantOpenProposals);
  const streaming = useAssistantStore(selectAssistantStreaming);
  const replying = useAssistantStore(selectAssistantReplying);
  const loaded = useAssistantStore(selectAssistantLoaded);
  const error = useAssistantStore(selectAssistantError);
  const enabled = useFeatureStore((s) => s.features.assistant);
  const featuresLoaded = useFeatureStore((s) => s.loaded);

  const scrollRef = useRef<HTMLDivElement>(null);
  const atBottomRef = useRef(true);

  // How much was unseen when the reader arrived, read once before the look
  // below zeroes it: the divider marks where this visit's news starts, and a
  // push landing while the page is open is not a reason to move it.
  const [unseenOnArrival] = useState(() => useAssistantStore.getState().unseen);

  const items = useMemo(
    () => buildTimeline({ messages, journal: entries, proposals, unseenOnArrival }),
    [messages, entries, proposals, unseenOnArrival],
  );

  // The conversation and the look, on arrival. Both are cheap and bounded, and
  // the look is what the page pins at its top, so it is not deferred behind the
  // first paint of the transcript.
  useEffect(() => {
    if (!enabled) return;
    const store = useAssistantStore.getState();
    store.setLoading(true);
    history(ws)
      .then((page) => store.setHistory(page))
      .catch((err) => {
        store.setLoading(false);
        store.setError(getErrorMessage(err, "Could not load the conversation"));
      });
    journal(ws)
      .then((entries) => store.applyJournal(entries))
      // A read that fails is missing news, not a broken page: the
      // conversation is what the operator came for. Logged, not toasted.
      .catch((err) => console.error("assistant.journal failed", err));
  }, [ws, enabled]);

  // On screen is seen. The look is taken on arrival, and installed in the store
  // so a journal push that lands while the thread is in front of the reader is
  // acknowledged as it arrives (see `applyAssistantJournal`): the rail's notch
  // goes locally at once, and the server stamps the journal through its newest
  // row. Leaving uninstalls it, so a push then counts as unseen again.
  useEffect(() => {
    if (!enabled) return;
    const look = () => {
      useAssistantStore.getState().markSeen();
      markSeenRpc(ws).catch((err) => console.warn("[assistant] mark-seen failed", err));
    };
    const store = useAssistantStore.getState();
    store.setViewing(true);
    store.setLook(look);
    look();
    return () => {
      const s = useAssistantStore.getState();
      s.setViewing(false);
      s.setLook(null);
    };
  }, [ws, enabled]);

  const handleScroll = useCallback((e: UIEvent<HTMLDivElement>) => {
    const el = e.currentTarget;
    atBottomRef.current = el.scrollTop + el.clientHeight >= el.scrollHeight - 40;
  }, []);

  // Follow the bottom while the reader is at it, and not otherwise — a reply
  // streams for as long as a turn takes, and yanking someone back down while
  // they read an older turn is the worse failure.
  // The lengths, not the arrays: a re-read that changes neither is not new
  // content, and the streaming reply grows one delta at a time.
  const itemCount = items.length;
  const streamedLength = streaming?.length ?? -1;
  useEffect(() => {
    if (itemCount === 0 && streamedLength < 0) return;
    if (!atBottomRef.current) return;
    const el = scrollRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [itemCount, streamedLength]);

  const visibleCards = useVisibleProposalCards(scrollRef, items);
  const pinned = useMemo(
    () => pinnedProposals(openProposals, visibleCards),
    [openProposals, visibleCards],
  );

  const showCard = useCallback((id: string) => {
    const card = scrollRef.current?.querySelector<HTMLElement>(
      `[data-proposal-id="${CSS.escape(id)}"]`,
    );
    if (!card) return;
    atBottomRef.current = false;
    const reduce = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    card.scrollIntoView({ block: "center", behavior: reduce ? "auto" : "smooth" });
  }, []);

  const handleSend = useCallback(
    async (text: string) => {
      const store = useAssistantStore.getState();
      // Armed before the round trip, because the head owes an answer from the
      // moment the ask is away — not from the moment its first token lands.
      store.beginReply();
      atBottomRef.current = true;
      try {
        const ask = await say(ws, text);
        if (ask) store.appendMessage(ask);
      } catch (err) {
        // The ask never landed, so nothing is coming: release the gate rather
        // than leaving the composer shut on a reply that will never arrive.
        store.clearStreaming();
        toast.error(getErrorMessage(err, "The assistant did not take that"));
        throw err;
      }
    },
    [ws],
  );

  if (featuresLoaded && !enabled) {
    return (
      <div className="flex flex-col h-full">
        <AssistantThreadHeader />
        <div className="flex-1 flex items-center justify-center px-6 text-center">
          <p className="text-sm text-muted-foreground max-w-sm">
            The assistant is off on this machine. Set <code>[experimental] assistant</code> in the
            server config to turn it on.
          </p>
        </div>
      </div>
    );
  }

  const empty = loaded && messages.length === 0 && streaming === null;

  return (
    <div className="flex flex-col h-full">
      <AssistantThreadHeader />
      <div
        ref={scrollRef}
        onScroll={handleScroll}
        className="flex-1 overflow-y-auto overflow-x-hidden"
      >
        {error && !loaded ? (
          <p className="text-xs text-destructive text-center py-6">{error}</p>
        ) : empty ? (
          <EmptyThread />
        ) : (
          <AssistantConversation items={items} streaming={streaming} />
        )}
      </div>
      <AssistantPinnedProposals proposals={pinned} onShow={showCard} />
      <AssistantComposer replying={replying} onSend={handleSend} />
    </div>
  );
}

const NO_CARDS: ReadonlySet<string> = new Set();

/**
 * The ids of the open proposal cards currently inside the scroller's view.
 *
 * One observer over every `[data-proposal-open]` card, rebuilt when the
 * timeline changes — cheap, since a thread holds a handful of cards, and it
 * keeps the set honest about cards that left the DOM. A card counts as visible
 * once any of it shows, because the pin exists for a card that cannot be seen at
 * all.
 */
function useVisibleProposalCards(
  scrollRef: React.RefObject<HTMLDivElement | null>,
  items: unknown,
): ReadonlySet<string> {
  const [visible, setVisible] = useState<ReadonlySet<string>>(NO_CARDS);
  useEffect(() => {
    void items;
    const root = scrollRef.current;
    if (!root || typeof IntersectionObserver === "undefined") return;
    const cards = root.querySelectorAll<HTMLElement>("[data-proposal-open]");
    if (cards.length === 0) {
      setVisible(NO_CARDS);
      return;
    }
    const shown = new Set<string>();
    const observer = new IntersectionObserver(
      (records) => {
        for (const record of records) {
          const id = (record.target as HTMLElement).dataset.proposalId;
          if (!id) continue;
          if (record.isIntersecting) shown.add(id);
          else shown.delete(id);
        }
        setVisible(new Set(shown));
      },
      { root },
    );
    for (const card of cards) observer.observe(card);
    return () => observer.disconnect();
  }, [scrollRef, items]);
  return visible;
}

/** Open proposals whose card is out of sight, oldest first. */
function pinnedProposals(
  open: AssistantProposal[],
  visible: ReadonlySet<string>,
): AssistantProposal[] {
  return open
    .filter((row) => row.id && !visible.has(row.id))
    .sort((a, b) => (a.createdAt ?? "").localeCompare(b.createdAt ?? ""));
}

/** One line saying what this is, and an invitation. Nothing else fits here. */
function EmptyThread() {
  return (
    <div className="h-full flex flex-col items-center justify-center gap-2 px-6 text-center">
      <HaloOrb size={32} state="idle" glyph="none" className="mb-1" />
      <p className="text-sm text-foreground">
        The assistant watches your sessions, remembers what you tell it, and can start work for you.
      </p>
      <p className="text-xs text-muted-foreground">
        Say what you are trying to get done — it is the same assistant you talk to on a call.
      </p>
    </div>
  );
}
