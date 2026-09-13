import { useEffect } from "react";
import type { useWebSocket } from "~/hooks/useWebSocket";
import {
  applyAssistantDelta,
  applyAssistantJournal,
  applyAssistantMessage,
  applyAssistantPolicy,
  applyAssistantProposal,
} from "~/lib/assistant/apply-push";
import { policies, proposals, unseen } from "~/lib/assistant/rpc";
import { useAssistantStore } from "~/stores/assistant-store";
import { useFeatureStore } from "~/stores/feature-store";

/**
 * Subscribes the assistant's global pushes, from the app shell, on the brain
 * subscriptions' precedent: they are not the thread page's to own. A reply
 * streams for as long as a turn takes, and navigating away mid-turn must not
 * lose the answer — the page renders what this has already collected.
 *
 * Mounted whether or not the feature is on. The server pushes nothing when the
 * assistant is unbuilt, so the cost of subscribing is five map entries, where
 * gating it on `features.assistant` would miss every push that lands before
 * `/api/health` answers.
 */
export function useAssistantSubscriptions(ws: ReturnType<typeof useWebSocket>) {
  const enabled = useFeatureStore((s) => s.features.assistant);

  // The rail row's notch and the open proposals, seeded once the feature is
  // known to be on and again on every reconnect: the pushes keep both current
  // in between, and a socket that was away has missed some. Gated here rather
  // than in the subscriptions, because a read against a peer with the assistant
  // off is a refusal in the console for nothing.
  //
  // The proposals are seeded HERE rather than on the thread, because the deck
  // lists them too and the landing page is where somebody arrives: a card that
  // only appears once you have opened `/assistant` is a yes nobody is asked
  // for.
  useEffect(() => {
    if (!enabled) return;
    const seed = () => {
      unseen(ws)
        .then((n) => useAssistantStore.getState().setUnseen(n))
        .catch((err) => console.warn("[assistant] unseen count unavailable", err));
      proposals(ws)
        .then((rows) => useAssistantStore.getState().applyProposals(rows))
        .catch((err) => console.warn("[assistant] proposals unavailable", err));
      // The policies ride the same seed for the same reason the proposals do:
      // they are read by a page somebody navigates to, and a page that fetches
      // its own list shows an empty table for a round trip every time it is
      // opened. The push keeps them current in between.
      policies(ws)
        .then((rows) => useAssistantStore.getState().setPolicies(rows))
        .catch((err) => console.warn("[assistant] policies unavailable", err));
    };
    seed();
    return ws.onConnect(seed);
  }, [ws, enabled]);

  useEffect(() => {
    const unsubMessage = ws.subscribe("assistant.message", applyAssistantMessage);
    const unsubDelta = ws.subscribe("assistant.delta", applyAssistantDelta);
    const unsubJournal = ws.subscribe("assistant.journal", applyAssistantJournal);
    const unsubProposal = ws.subscribe("assistant.proposal", applyAssistantProposal);
    const unsubPolicy = ws.subscribe("assistant.policy", applyAssistantPolicy);
    // A reconnect drops whatever the head was mid-way through saying: the turn
    // it belonged to was reaped or is finishing into a socket that is gone, and
    // a partial reply left on screen would never be replaced by its stored
    // form. The conversation itself is server state and reloads with the page.
    const unsubConnect = ws.onConnect(() => {
      useAssistantStore.getState().clearStreaming();
    });
    return () => {
      unsubMessage();
      unsubDelta();
      unsubJournal();
      unsubProposal();
      unsubPolicy();
      unsubConnect();
    };
  }, [ws]);
}
