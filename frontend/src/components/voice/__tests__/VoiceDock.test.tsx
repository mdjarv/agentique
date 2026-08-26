/**
 * The rule a long line of dictation keeps breaking.
 *
 * Interim transcript is an unbounded run of words with no whitespace budget and
 * no upper length, and it arrives in a fixed-width rail. Every flex box between
 * the card and the caption therefore has to be `min-w-0`, or one of them sizes
 * itself to its content and the card grows past the sidebar. A screenshot test
 * would catch that; this catches it by construction, which is the form the rule
 * is actually stated in.
 */
import "@testing-library/jest-dom/vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { VoiceDock } from "~/components/voice/VoiceDock";
import { VoiceStrip } from "~/components/voice/VoiceStrip";
import { useAppStore } from "~/stores/app-store";
import { useChatStore } from "~/stores/chat-store";
import { useFeatureStore } from "~/stores/feature-store";
import { useVoiceStore } from "~/stores/voice-store";

/** One breath of dictation, as it arrives: no punctuation, no natural break. */
const LONG_INTERIM =
  "so what I want you to do is go and look at the reconnect path in the machine " +
  "registry and work out why the paired laptop keeps coming back as reconnecting " +
  "even after the credential has been accepted again";

const LONG_NAME = "Live Voice Dialog — switchboard, dock, strip and the halo that ties them";

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

/** A live call, mid-sentence, focused on a session with a long name. */
function liveCall() {
  useFeatureStore.setState({ features: { browser: false, teams: false, voice: true } });
  useChatStore.setState({
    activeSessionId: "s1",
    sessions: {
      s1: {
        meta: { id: "s1", name: LONG_NAME },
      },
    },
  } as never);
  useVoiceStore.setState({
    status: "live",
    detail: undefined,
    activityLabel: "",
    interim: { source: "you", text: LONG_INTERIM },
    focusSessionId: "s1",
    focusSeq: 1,
    log: [],
  });
}

/**
 * Walks up from the caption to the card, asserting nothing on the way can be
 * pushed wide by its content.
 */
function expectNarrowPath(from: HTMLElement, stopClass: string) {
  expect(from.className).toContain("truncate");
  let node: HTMLElement | null = from;
  let sawStop = false;
  while (node) {
    if (node.className.includes(stopClass)) {
      sawStop = true;
      break;
    }
    expect(node.className).toContain("min-w-0");
    node = node.parentElement;
  }
  expect(sawStop).toBe(true);
}

afterEach(() => {
  cleanup();
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

describe("VoiceDock", () => {
  beforeEach(() => {
    mockMatchMedia(false);
    window.innerWidth = 1280;
    liveCall();
  });

  it("keeps a whole breath of dictation inside the card", () => {
    render(<VoiceDock />);
    expectNarrowPath(screen.getByText(LONG_INTERIM), "bg-popover");
  });

  it("names the call, not the session — the session is the chip", () => {
    render(<VoiceDock />);
    expect(screen.getByText("Live call")).toBeInTheDocument();
    const chip = screen.getByText(`▸ ${LONG_NAME}`);
    expect(chip.className).toContain("truncate");
    expect(chip.className).toContain("max-w-[46%]");
  });

  it("is a trace with no call, and offers the shortcut that does the same", () => {
    useVoiceStore.setState({ status: "idle", interim: null, focusSessionId: null });
    render(<VoiceDock />);
    expect(screen.getByRole("button", { name: "Start live call" })).toBeInTheDocument();
    expect(screen.getByText("⌥V")).toBeInTheDocument();
  });
});

/**
 * The phone splits the dock in two, and the split is the whole design: the way
 * *in* is navigation and belongs in the drawer, a live call is the thing that is
 * happening and belongs on the strip in front of it. Before this, mobile got
 * neither — the dock returned null outright, so the only way to start a call was
 * the composer's Live button, which is tied to a session.
 */
describe("VoiceDock on a phone", () => {
  beforeEach(() => {
    mockMatchMedia(true);
    window.innerWidth = 390;
    liveCall();
  });

  it("carries the way in when there is no call", () => {
    useVoiceStore.setState({ status: "idle", interim: null, focusSessionId: null });
    render(<VoiceDock />);
    expect(screen.getByRole("button", { name: "Start live call" })).toBeInTheDocument();
  });

  it("does not advertise a shortcut the phone has no keys for", () => {
    useVoiceStore.setState({ status: "idle", interim: null, focusSessionId: null });
    render(<VoiceDock />);
    expect(screen.queryByText("⌥V")).not.toBeInTheDocument();
  });

  it("renders nothing at all once a call exists — the strip owns it", () => {
    const { container } = render(<VoiceDock />);
    expect(container).toBeEmptyDOMElement();
    expect(screen.queryByRole("button", { name: "End call" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Start live call" })).not.toBeInTheDocument();
  });

  it("leaves an ended call to the strip too", () => {
    useVoiceStore.setState({ status: "ended" });
    const { container } = render(<VoiceDock />);
    expect(container).toBeEmptyDOMElement();
  });

  it("closes the drawer it is in when the call starts", () => {
    const start = vi.fn();
    const realStart = useVoiceStore.getState().start;
    useAppStore.setState({ sidebarOpen: true });
    useVoiceStore.setState({ status: "idle", interim: null, focusSessionId: null, start });

    render(<VoiceDock />);
    fireEvent.click(screen.getByRole("button", { name: "Start live call" }));

    expect(start).toHaveBeenCalled();
    expect(useAppStore.getState().sidebarOpen).toBe(false);
    useVoiceStore.setState({ start: realStart });
  });
});

describe("VoiceStrip", () => {
  beforeEach(() => {
    mockMatchMedia(true);
    window.innerWidth = 390;
    liveCall();
  });

  it("keeps a whole breath of dictation inside the strip", () => {
    render(<VoiceStrip />);
    expectNarrowPath(screen.getByText(LONG_INTERIM), "bg-popover");
  });

  it("opens the log rather than acting on the call when the caption is tapped", () => {
    render(<VoiceStrip />);
    expect(screen.getByRole("button", { name: "Open the call" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "End call" })).toBeInTheDocument();
  });
});
