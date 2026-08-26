/**
 * Dictation, driven through the composer's own base-snapshot layer, because the
 * bug this file exists for was only visible at the seam between the two.
 *
 * The fake below is Chrome: one result entry per utterance, updated in place
 * while it is a guess and frozen when it goes final, and an `onend` the browser
 * fires whenever it feels like it — after every utterance on Android, every few
 * seconds of speech on the desktop. The audio behind an uncommitted guess is
 * re-offered to whatever session comes next, so the fake re-delivers it too.
 */
import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useComposerSpeech } from "~/components/chat/composer/useComposerSpeech";

interface FakeResult {
  0: { transcript: string };
  isFinal: boolean;
}

const { instances, FakeRecognition } = vi.hoisted(() => {
  const instances: FakeRecognition[] = [];

  class FakeRecognition {
    continuous = false;
    interimResults = false;
    lang = "";
    maxAlternatives = 1;
    onresult: ((event: { results: FakeResult[] }) => void) | null = null;
    onerror: ((event: { error: string; message: string }) => void) | null = null;
    onend: ((event: Event) => void) | null = null;
    onstart: ((event: Event) => void) | null = null;

    state: "idle" | "running" | "stopped" | "ended" | "aborted" = "idle";
    results: FakeResult[] = [];

    constructor() {
      instances.push(this);
    }

    start() {
      if (this.state === "running") throw new Error("InvalidStateError");
      this.state = "running";
    }

    /** Cooperative stop: the engine winds down and reports it. */
    stop() {
      if (this.state !== "running") return;
      this.state = "stopped";
      this.onend?.(new Event("end"));
    }

    abort() {
      this.state = "aborted";
    }

    addEventListener() {}
    removeEventListener() {}
    dispatchEvent() {
      return true;
    }

    // --- what the engine does to us ---

    /** A guess at the words being spoken now; replaces the last one. */
    interim(transcript: string) {
      const last = this.results.at(-1);
      if (last && !last.isFinal) last[0] = { transcript };
      else this.results.push({ 0: { transcript }, isFinal: false });
      this.emit();
    }

    /** The engine commits the current utterance. */
    final(transcript: string) {
      const last = this.results.at(-1);
      if (last && !last.isFinal) {
        last[0] = { transcript };
        last.isFinal = true;
      } else {
        this.results.push({ 0: { transcript }, isFinal: true });
      }
      this.emit();
    }

    /** The browser ends the session on its own, mid-dictation. */
    end() {
      if (this.state !== "running") return;
      this.state = "ended";
      this.onend?.(new Event("end"));
    }

    error(error: string) {
      this.onerror?.({ error, message: "" });
    }

    private emit() {
      this.onresult?.({ results: this.results.map((r) => ({ ...r })) });
    }
  }

  (globalThis as unknown as { SpeechRecognition: unknown }).SpeechRecognition = FakeRecognition;
  return { instances, FakeRecognition };
});

type Rec = InstanceType<typeof FakeRecognition>;

/** The recognizers still holding the microphone. There must never be two. */
function live(): Rec[] {
  return instances.filter((i) => i.state === "running");
}

function newest(): Rec {
  const rec = instances.at(-1);
  if (!rec) throw new Error("no recognizer was created");
  return rec;
}

/** The composer, reduced to the two things dictation touches. */
function mountComposerSpeech(initialText = "") {
  const text = { current: initialText };
  const getText = vi.fn(() => text.current);
  const hook = renderHook(() =>
    useComposerSpeech({
      getText,
      setText: (value: string) => {
        text.current = value;
      },
    }),
  );
  return { text, getText, ...hook };
}

beforeEach(() => {
  instances.length = 0;
  vi.spyOn(console, "warn").mockImplementation(() => {});
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("dictation as one span", () => {
  it("replaces the interim rather than piling onto it", () => {
    const { text, result } = mountComposerSpeech();
    act(() => result.current.toggle());

    const rec = newest();
    act(() => rec.interim("I"));
    act(() => rec.interim("I tried"));
    act(() => rec.interim("I tried opening"));
    act(() => rec.final("I tried opening a voice call"));

    expect(text.current).toBe("I tried opening a voice call");
  });

  it("continues the span when the browser ends the session mid-sentence", () => {
    // The reported bug: Android Chrome ends recognition at every pause. Each end
    // used to idle the mic, and the press that resumed re-snapshotted a base
    // that already held the dictated words.
    const { text, getText, result } = mountComposerSpeech();
    act(() => result.current.toggle());

    const first = newest();
    act(() => first.final("I tried"));
    act(() => first.interim("opening a"));
    expect(text.current).toBe("I tried opening a");

    act(() => first.end());

    // Still dictating, on a fresh recognizer, with the base untouched.
    expect(result.current.isListening).toBe(true);
    expect(instances).toHaveLength(2);
    expect(live()).toHaveLength(1);
    expect(getText).toHaveBeenCalledTimes(1);
    // The guess the engine never committed is off screen: it is about to be
    // re-offered, and keeping it is what printed those words twice.
    expect(text.current).toBe("I tried");

    const second = newest();
    act(() => second.interim("opening a voice"));
    act(() => second.final("opening a voice call"));

    expect(text.current).toBe("I tried opening a voice call");
  });

  it("survives a session ending after every single utterance", () => {
    const { text, getText, result } = mountComposerSpeech();
    act(() => result.current.toggle());

    for (const phrase of ["I tried", "opening a", "voice call"]) {
      const rec = newest();
      act(() => rec.interim(phrase));
      act(() => rec.final(phrase));
      act(() => rec.end());
    }

    expect(text.current).toBe("I tried opening a voice call");
    // The shape of the field report: every restart re-appending what the field
    // already held — "I I I tried I tried I tried opening…".
    expect(text.current).not.toContain("I tried I tried");
    expect(getText).toHaveBeenCalledTimes(1);
    expect(live()).toHaveLength(1);
    expect(result.current.isListening).toBe(true);
  });

  it("never leaves two recognizers holding the microphone", () => {
    const { result } = mountComposerSpeech();
    act(() => result.current.toggle());
    for (let i = 0; i < 3; i++) {
      const rec = newest();
      act(() => rec.final(`part ${i}`));
      act(() => rec.end());
      expect(live()).toHaveLength(1);
    }
  });
});

describe("dictation against existing text", () => {
  it("appends after the text already in the composer, one space between", () => {
    const { text, result } = mountComposerSpeech("note:");
    act(() => result.current.toggle());
    act(() => newest().final("hello"));

    expect(text.current).toBe("note: hello");
  });

  it("appends again when a second dictation is started", () => {
    const { text, getText, result } = mountComposerSpeech("note:");

    act(() => result.current.toggle());
    act(() => newest().final("hello"));
    act(() => result.current.toggle());
    expect(result.current.isListening).toBe(false);

    act(() => result.current.toggle());
    act(() => newest().final("world"));

    expect(text.current).toBe("note: hello world");
    expect(text.current).not.toMatch(/ {2}/);
    // One snapshot per dictation the operator started — two here, not four.
    expect(getText).toHaveBeenCalledTimes(2);
  });

  it("keeps a trailing space in the base from doubling", () => {
    const { text, result } = mountComposerSpeech("note: ");
    act(() => result.current.toggle());
    act(() => newest().final(" hello "));

    expect(text.current).toBe("note: hello");
  });
});

describe("when the span really is over", () => {
  it("gives up after a run of sessions that hear nothing", () => {
    const { result } = mountComposerSpeech();
    act(() => result.current.toggle());

    for (let i = 0; i < 3; i++) act(() => newest().end());

    expect(result.current.isListening).toBe(false);
    expect(instances).toHaveLength(3);
  });

  it("refills the budget the moment the engine hears something", () => {
    const { result } = mountComposerSpeech();
    act(() => result.current.toggle());

    act(() => newest().end());
    act(() => newest().end());
    act(() => newest().final("still here"));
    act(() => newest().end());
    act(() => newest().end());

    expect(result.current.isListening).toBe(true);
  });

  it("stops on an error no restart can get past", () => {
    const { result } = mountComposerSpeech();
    act(() => result.current.toggle());
    act(() => newest().error("not-allowed"));

    expect(result.current.isListening).toBe(false);
    expect(instances).toHaveLength(1);
  });

  it("rides out a transient network error", () => {
    const { result } = mountComposerSpeech();
    act(() => result.current.toggle());
    const rec = newest();
    act(() => rec.error("network"));
    act(() => rec.end());

    expect(result.current.isListening).toBe(true);
    expect(instances).toHaveLength(2);
  });

  it("does not restart after the operator stops it", () => {
    const { result } = mountComposerSpeech();
    act(() => result.current.toggle());
    act(() => newest().final("done"));
    act(() => result.current.toggle());

    expect(result.current.isListening).toBe(false);
    expect(instances).toHaveLength(1);
  });

  it("does not restart into an unmounted composer", () => {
    const { result, unmount } = mountComposerSpeech();
    act(() => result.current.toggle());
    unmount();

    expect(instances).toHaveLength(1);
    expect(live()).toHaveLength(0);
  });
});
