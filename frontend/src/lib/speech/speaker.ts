import { toSpeakableText, toUtteranceChunks } from "./speakable";

/**
 * Reading a message aloud, via the browser's own speech synthesis.
 *
 * Deliberately local rather than routed through the live voice backend: this is
 * a utility on a button, and it should work instantly, offline, with no key
 * configured and nothing billed. It will not sound like the Live agent, and
 * that is the right trade for a control you might press on any message.
 *
 * The speaker is a module singleton because speech is a **serial channel**.
 * There is one pair of speakers and one listener, so starting a message stops
 * whatever was already speaking; per-component state would let two answers talk
 * over each other.
 */

/** Chrome stops speaking after ~15s unless resume() is called periodically. */
const KEEPALIVE_MS = 10_000;

type Listener = (speakingId: string | null) => void;

class Speaker {
  private listeners = new Set<Listener>();
  private currentId: string | null = null;
  private keepalive: ReturnType<typeof setInterval> | null = null;

  /**
   * Guards against a cancelled utterance's onend arriving after the next one
   * started and clearing the wrong id.
   */
  private generation = 0;

  get supported(): boolean {
    return typeof window !== "undefined" && "speechSynthesis" in window;
  }

  get speakingId(): string | null {
    return this.currentId;
  }

  subscribe(listener: Listener): () => void {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  }

  /** Speaks markdown under an id, or stops if that id is already speaking. */
  toggle(id: string, markdown: string): void {
    if (this.currentId === id) {
      this.stop();
      return;
    }
    this.speak(id, markdown);
  }

  speak(id: string, markdown: string): void {
    if (!this.supported) return;

    // Whatever was speaking loses the channel.
    this.cancel();

    const chunks = toUtteranceChunks(toSpeakableText(markdown));
    if (chunks.length === 0) return;

    const generation = ++this.generation;
    this.setSpeaking(id);
    this.startKeepalive();

    chunks.forEach((chunk, index) => {
      const utterance = new SpeechSynthesisUtterance(chunk);
      if (index === chunks.length - 1) {
        utterance.onend = () => {
          if (generation !== this.generation) return;
          this.finish();
        };
      }
      utterance.onerror = (event) => {
        if (generation !== this.generation) return;
        // "interrupted"/"canceled" are what a deliberate stop looks like.
        if (event.error !== "interrupted" && event.error !== "canceled") {
          console.warn("[speech] utterance failed:", event.error);
        }
        this.finish();
      };
      window.speechSynthesis.speak(utterance);
    });
  }

  stop(): void {
    this.cancel();
    this.finish();
  }

  private cancel(): void {
    if (!this.supported) return;
    this.generation++;
    window.speechSynthesis.cancel();
  }

  private finish(): void {
    this.stopKeepalive();
    this.setSpeaking(null);
  }

  private setSpeaking(id: string | null): void {
    if (this.currentId === id) return;
    this.currentId = id;
    for (const listener of this.listeners) listener(id);
  }

  private startKeepalive(): void {
    this.stopKeepalive();
    this.keepalive = setInterval(() => {
      if (!window.speechSynthesis.speaking) return;
      // pause/resume is the documented workaround for Chrome's ~15s cutoff.
      window.speechSynthesis.pause();
      window.speechSynthesis.resume();
    }, KEEPALIVE_MS);
  }

  private stopKeepalive(): void {
    if (this.keepalive === null) return;
    clearInterval(this.keepalive);
    this.keepalive = null;
  }
}

export const speaker = new Speaker();
