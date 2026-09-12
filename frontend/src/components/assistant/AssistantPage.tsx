import { type UIEvent, useCallback, useEffect, useRef } from "react";
import { toast } from "sonner";
import { AssistantComposer } from "~/components/assistant/AssistantComposer";
import { AssistantConversation } from "~/components/assistant/AssistantConversation";
import { AssistantThreadHeader } from "~/components/assistant/AssistantHeader";
import { AssistantProposals } from "~/components/assistant/AssistantProposals";
import { AssistantUpdatesStrip } from "~/components/assistant/AssistantUpdatesStrip";
import { HaloOrb } from "~/components/voice/HaloOrb";
import { useWebSocket } from "~/hooks/useWebSocket";
import { history, journal, markSeen as markSeenRpc, say } from "~/lib/assistant/rpc";
import { getErrorMessage } from "~/lib/utils";
import {
  selectAssistantError,
  selectAssistantJournal,
  selectAssistantLoaded,
  selectAssistantMessages,
  selectAssistantReplying,
  selectAssistantStreaming,
  useAssistantStore,
} from "~/stores/assistant-store";
import { useFeatureStore } from "~/stores/feature-store";

/**
 * The thread — `/assistant`, the page the rail's assistant row opens.
 *
 * A TRANSPORT, not a head: it brings no model. It renders the shared
 * conversation, forwards the operator's text to the core's own head, and pins
 * what happened while nobody was looking above it. Everything that decides
 * anything — refusals, tiers, budgets — is in `internal/assistant`, where the
 * call hits the same rules.
 *
 * Mobile renders this page, not a variant of it: the strip, the conversation
 * and the composer are one column at every width, and the composer goes flush
 * to the edges on a phone the way a session's does.
 */
export function AssistantPage() {
  const ws = useWebSocket();
  const messages = useAssistantStore(selectAssistantMessages);
  const entries = useAssistantStore(selectAssistantJournal);
  const streaming = useAssistantStore(selectAssistantStreaming);
  const replying = useAssistantStore(selectAssistantReplying);
  const loaded = useAssistantStore(selectAssistantLoaded);
  const error = useAssistantStore(selectAssistantError);
  const enabled = useFeatureStore((s) => s.features.assistant);
  const featuresLoaded = useFeatureStore((s) => s.loaded);

  const scrollRef = useRef<HTMLDivElement>(null);
  const atBottomRef = useRef(true);

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
      // A read that fails is a missing strip, not a broken page: the
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
  const messageCount = messages.length;
  const streamedLength = streaming?.length ?? -1;
  useEffect(() => {
    if (messageCount === 0 && streamedLength < 0) return;
    if (!atBottomRef.current) return;
    const el = scrollRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [messageCount, streamedLength]);

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
      <AssistantUpdatesStrip entries={entries} />
      <AssistantProposals />
      <div ref={scrollRef} onScroll={handleScroll} className="flex-1 overflow-y-auto">
        {error && !loaded ? (
          <p className="text-xs text-destructive text-center py-6">{error}</p>
        ) : empty ? (
          <EmptyThread />
        ) : (
          <AssistantConversation messages={messages} streaming={streaming} />
        )}
      </div>
      <AssistantComposer replying={replying} onSend={handleSend} />
    </div>
  );
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
