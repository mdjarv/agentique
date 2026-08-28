/**
 * Hands-free, as the rules it exists for.
 *
 * Every assertion here is one of the things a driver cannot do: read 11px type,
 * find a low-contrast tint through a windscreen, hit a 36px target without
 * looking, scroll, or press a button that moved since the last glance. They are
 * asserted rather than eyeballed because all five degrade silently — the screen
 * still renders, it just stops being usable at 70 mph, which is not a state a
 * screenshot review catches either.
 */
import "@testing-library/jest-dom/vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { VoiceStrip } from "~/components/voice/VoiceStrip";
import { useChatStore } from "~/stores/chat-store";
import { useFeatureStore } from "~/stores/feature-store";
import { useUIStore } from "~/stores/ui-store";
import { useVoiceStore } from "~/stores/voice-store";

const SESSION_NAME = "Reconnect Drops";

function mockMatchMedia(matches: boolean) {
  Object.defineProperty(window, "matchMedia", {
    writable: true,
    value: vi.fn().mockImplementation((query: string) => ({
      matches,
      media: query,
      onchange: null,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(),
    })),
  });
}

function liveCall() {
  useFeatureStore.setState({ features: { browser: false, teams: false, voice: true } });
  useChatStore.setState({
    activeSessionId: "s1",
    sessions: { s1: { meta: { id: "s1", name: SESSION_NAME } } },
  } as never);
  useVoiceStore.setState({
    status: "live",
    detail: undefined,
    activityLabel: "",
    interim: null,
    focusSessionId: "s1",
    focusSeq: 1,
    log: [],
  });
}

/** The px height a Tailwind arbitrary class encodes, or 0 when there is none. */
function heightPx(el: HTMLElement): number {
  const match = /h-\[(\d+)px\]/.exec(el.className);
  return match ? Number(match[1]) : 0;
}

beforeEach(() => {
  mockMatchMedia(true);
  window.innerWidth = 390;
  liveCall();
  useUIStore.setState({ handsFree: true });
});

afterEach(() => {
  cleanup();
  useUIStore.setState({ handsFree: false });
  useVoiceStore.setState({
    status: "idle",
    detail: undefined,
    activityLabel: "",
    interim: null,
    focusSessionId: null,
    focusSeq: 0,
    log: [],
  });
});

describe("the hands-free call surface", () => {
  it("replaces the strip rather than sitting beside it", () => {
    render(<VoiceStrip />);
    expect(screen.getByRole("dialog", { name: "Hands-free call" })).toBeInTheDocument();
    // The strip's own controls are gone: tucking a call away and opening its
    // log are both ways of getting to something else on screen, and there is
    // nothing else on screen.
    expect(screen.queryByRole("button", { name: "Tuck the call away" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Open the call" })).toBeNull();
  });

  // The number that decides whether this feature works at all.
  it("gives the one control a target far past the platform minimum", () => {
    render(<VoiceStrip />);
    const end = screen.getByRole("button", { name: "End call" });
    expect(heightPx(end)).toBeGreaterThanOrEqual(88);
  });

  it("fills the control solidly instead of washing it", () => {
    render(<VoiceStrip />);
    const end = screen.getByRole("button", { name: "End call" });
    // `bg-destructive/10` is the app's ordinary treatment and is unfindable
    // through glass in daylight. A solid fill has no opacity suffix.
    expect(end.className).toContain("bg-destructive");
    expect(end.className).not.toMatch(/bg-destructive\/\d/);
  });

  it("spells the control as well as drawing it", () => {
    render(<VoiceStrip />);
    // A glyph alone is a thing to decode, which is the one thing this surface
    // must never ask for.
    expect(screen.getByText("End call")).toBeInTheDocument();
  });

  it("names the session a prompt would land in, at its own size", () => {
    render(<VoiceStrip />);
    const chip = screen.getByText(`▸ ${SESSION_NAME}`);
    // Second-largest thing on the screen, and not the strip's 10px chip. It
    // outranks the status line below it, which is the hierarchy: what a prompt
    // would hit matters more at a glance than what the call is busy with.
    expect(chip.className).toContain("text-xl");
  });

  it("says so plainly when nothing is aimed at yet", () => {
    useVoiceStore.setState({ focusSessionId: null });
    render(<VoiceStrip />);
    expect(screen.getByText("No session yet")).toBeInTheDocument();
  });

  it("clamps a long spoken line rather than offering a scroll", () => {
    const said = "go and look at the reconnect path in the machine registry and tell me why";
    useVoiceStore.setState({ log: [{ id: 1, at: 0, source: "you", text: said }] as never });
    render(<VoiceStrip />);
    // Scrolling needs a second look at the screen; a cut-off line does not, and
    // the call says the whole thing out loud anyway.
    expect(screen.getByText(said).closest("[class*='line-clamp']")).not.toBeNull();
  });

  // `line-clamp` sets `display: -webkit-box`, which takes a flex row apart. The
  // working line is one — a spinner beside a label — and it lost its spinner to
  // the top-left corner before the clamp learned to skip it.
  it("leaves the working line's spinner row intact", () => {
    useVoiceStore.setState({ activityLabel: "Summarising the session you asked about" });
    render(<VoiceStrip />);
    const label = screen.getByText("Summarising the session you asked about");
    const row = label.parentElement;
    expect(row?.className).toContain("items-center");
    expect(row?.className).not.toContain("line-clamp");
  });

  // A call can end between the look and the press. If the primary control
  // changed size or moved, that press lands on whatever took its place.
  it("keeps the control the same size when the call ends", () => {
    const { rerender } = render(<VoiceStrip />);
    const live = heightPx(screen.getByRole("button", { name: "End call" }));

    useVoiceStore.setState({ status: "ended" });
    rerender(<VoiceStrip />);
    const ended = heightPx(screen.getByRole("button", { name: "Call again" }));

    expect(ended).toBe(live);
  });

  it("leaves by a control sized like the rest of the screen", () => {
    render(<VoiceStrip />);
    const exit = screen.getByRole("button", { name: "Leave hands-free" });
    expect(exit.className).toContain("size-14");

    fireEvent.click(exit);
    expect(useUIStore.getState().handsFree).toBe(false);
  });

  it("is a phone surface only", () => {
    mockMatchMedia(false);
    window.innerWidth = 1280;
    render(<VoiceStrip />);
    expect(screen.queryByRole("dialog", { name: "Hands-free call" })).toBeNull();
  });

  // Armed once, and it stays armed: the gesture that turns it on is not one to
  // make at the wheel, so it cannot be per call.
  it("is entered from the call sheet and remembered", () => {
    useUIStore.setState({ handsFree: false });
    render(<VoiceStrip />);

    fireEvent.click(screen.getByRole("button", { name: "Open the call" }));
    fireEvent.click(screen.getByRole("button", { name: /Hands-free/ }));

    expect(useUIStore.getState().handsFree).toBe(true);
    expect(screen.getByRole("dialog", { name: "Hands-free call" })).toBeInTheDocument();
  });
});
