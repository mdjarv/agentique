import { beforeEach, describe, expect, it } from "vitest";
import { useVoiceStore, voiceCallHandlers } from "~/stores/voice-store";

/** The store as a fresh call would find it. */
function reset() {
  useVoiceStore.setState({
    status: "idle",
    detail: undefined,
    activityLabel: "",
    interim: null,
    focusSessionId: null,
    focusSeq: 0,
    log: [],
  });
}

const h = voiceCallHandlers;

describe("voice-store frames", () => {
  beforeEach(reset);

  describe("activity", () => {
    it("a label says what the call is working on", () => {
      h.onActivity?.({ type: "activity", label: "Summarizing Live Voice Dialog" });
      expect(useVoiceStore.getState().activityLabel).toBe("Summarizing Live Voice Dialog");
    });

    it("a new label replaces the old one", () => {
      h.onActivity?.({ type: "activity", label: "first" });
      h.onActivity?.({ type: "activity", label: "second" });
      expect(useVoiceStore.getState().activityLabel).toBe("second");
    });

    it("an empty or absent label clears it", () => {
      h.onActivity?.({ type: "activity", label: "working" });
      h.onActivity?.({ type: "activity", label: "  " });
      expect(useVoiceStore.getState().activityLabel).toBe("");

      h.onActivity?.({ type: "activity", label: "working" });
      h.onActivity?.({ type: "activity" });
      expect(useVoiceStore.getState().activityLabel).toBe("");
    });

    it("does not survive the call leaving the live state", () => {
      h.onActivity?.({ type: "activity", label: "working" });
      h.onState("closed", "idle");
      expect(useVoiceStore.getState().activityLabel).toBe("");
    });
  });

  describe("summary", () => {
    it("lands in the log as its own source, with the session it is about", () => {
      h.onSummary?.({ type: "summary", sessionId: "s1", headline: "Tests were already broken." });
      const [entry] = useVoiceStore.getState().log;
      expect(entry?.source).toBe("summary");
      expect(entry?.text).toBe("Tests were already broken.");
      expect(entry?.sessionId).toBe("s1");
    });

    it("an empty summary is not logged", () => {
      h.onSummary?.({ type: "summary", sessionId: "s1" });
      expect(useVoiceStore.getState().log).toHaveLength(0);
    });
  });

  describe("transcript", () => {
    it("interim text is held aside, never logged", () => {
      h.onTranscript?.({
        type: "transcript",
        text: "summarize the",
        final: false,
        source: "caller",
      });
      expect(useVoiceStore.getState().interim).toEqual({ source: "you", text: "summarize the" });
      expect(useVoiceStore.getState().log).toHaveLength(0);
    });

    it("a later interim replaces the earlier one", () => {
      h.onTranscript?.({ type: "transcript", text: "summ", final: false, source: "caller" });
      h.onTranscript?.({ type: "transcript", text: "summarize", final: false, source: "caller" });
      expect(useVoiceStore.getState().interim?.text).toBe("summarize");
    });

    it("the final form logs the line and clears the provisional one", () => {
      h.onTranscript?.({
        type: "transcript",
        text: "summarize it",
        final: false,
        source: "caller",
      });
      h.onTranscript?.({
        type: "transcript",
        text: "Summarize it.",
        final: true,
        source: "caller",
      });
      const state = useVoiceStore.getState();
      expect(state.interim).toBeNull();
      expect(state.log).toHaveLength(1);
      expect(state.log[0]).toMatchObject({ source: "you", text: "Summarize it." });
    });

    it("anything that is not the caller is the agent talking", () => {
      h.onTranscript?.({ type: "transcript", text: "On it.", final: true, source: "model" });
      expect(useVoiceStore.getState().log[0]?.source).toBe("agent");
    });

    it("empty interim text clears rather than showing a blank line", () => {
      h.onTranscript?.({ type: "transcript", text: "half", final: false, source: "caller" });
      h.onTranscript?.({ type: "transcript", text: "", final: false, source: "caller" });
      expect(useVoiceStore.getState().interim).toBeNull();
    });
  });

  describe("ending", () => {
    it("a closed frame ends the call and keeps the reason", () => {
      h.onState("live");
      h.onState("closed", "idle");
      const state = useVoiceStore.getState();
      expect(state.status).toBe("ended");
      expect(state.detail).toBe("idle");
    });

    it("the socket closing behind the reason does not erase it", () => {
      h.onState("live");
      // The server's `closed` frame, then the socket close that follows it.
      h.onState("closed", "idle");
      h.onState("closed");
      expect(useVoiceStore.getState().detail).toBe("idle");
    });

    it("the call object reporting idle never speaks for the surfaces", () => {
      h.onState("live");
      h.onState("closed", "idle");
      h.onState("idle");
      expect(useVoiceStore.getState().status).toBe("ended");
    });

    it("an ended call keeps its log until it is dismissed", () => {
      h.onSummary?.({ type: "summary", headline: "the answer" });
      h.onState("closed", "idle");
      expect(useVoiceStore.getState().log).toHaveLength(1);

      useVoiceStore.getState().dismiss();
      const state = useVoiceStore.getState();
      expect(state.status).toBe("idle");
      expect(state.log).toHaveLength(0);
      expect(state.detail).toBeUndefined();
    });

    it("hanging up leaves the call on screen as ended, not gone", () => {
      h.onState("live");
      useVoiceStore.getState().stop();
      const state = useVoiceStore.getState();
      expect(state.status).toBe("ended");
      expect(state.detail).toBe("you ended the call");
    });

    it("hanging up with no call does not invent an ended one", () => {
      useVoiceStore.getState().stop();
      expect(useVoiceStore.getState().status).toBe("idle");
    });

    it("a failure keeps its message when the socket closes after it", () => {
      h.onState("live");
      h.onState("failed", "the microphone was disconnected");
      expect(useVoiceStore.getState().status).toBe("error");
      h.onState("closed");
      const state = useVoiceStore.getState();
      expect(state.status).toBe("ended");
      expect(state.detail).toBe("the microphone was disconnected");
    });

    it("connecting again clears the last call's epitaph", () => {
      h.onState("closed", "idle");
      h.onState("connecting");
      const state = useVoiceStore.getState();
      expect(state.status).toBe("connecting");
      expect(state.detail).toBeUndefined();
    });
  });

  describe("focus", () => {
    it("bumps a sequence so the same session can be asked for twice", () => {
      h.onFocus?.({ type: "focus", sessionId: "s1" });
      h.onFocus?.({ type: "focus", sessionId: "s1" });
      const state = useVoiceStore.getState();
      expect(state.focusSessionId).toBe("s1");
      expect(state.focusSeq).toBe(2);
    });

    it("ignores a focus frame with no session", () => {
      h.onFocus?.({ type: "focus" });
      expect(useVoiceStore.getState().focusSeq).toBe(0);
    });
  });
});
