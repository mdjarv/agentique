import "@testing-library/jest-dom/vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MessageComposer } from "~/components/chat/MessageComposer";

// Speech recognition isn't available in jsdom; mock the hook so the mic button
// renders and we can exercise the pointer-capture handling in useComposerSpeech.
const speechMock = vi.hoisted(() => ({
  isListening: false,
  isSupported: true,
  start: vi.fn(),
  stop: vi.fn(),
  forceStop: vi.fn(),
  toggle: vi.fn(),
}));

vi.mock("~/hooks/useSpeechRecognition", () => ({
  useSpeechRecognition: () => speechMock,
}));

function installBrowserMocks() {
  Object.defineProperty(window, "matchMedia", {
    writable: true,
    value: vi.fn().mockImplementation((query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      addListener: vi.fn(),
      removeListener: vi.fn(),
      dispatchEvent: vi.fn(),
    })),
  });

  if (!window.requestAnimationFrame) {
    window.requestAnimationFrame = (cb: FrameRequestCallback) =>
      window.setTimeout(() => cb(performance.now()), 0);
  }
}

describe("MessageComposer", () => {
  beforeEach(() => {
    installBrowserMocks();
    speechMock.isListening = false;
    speechMock.start.mockClear();
    speechMock.stop.mockClear();
    speechMock.forceStop.mockClear();
    speechMock.toggle.mockClear();
  });

  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  it("keeps the draft when sending fails", async () => {
    const onSend = vi.fn(async () => false);
    render(<MessageComposer projectId="project-1" onSend={onSend} />);

    const textarea = screen.getByPlaceholderText("Send a message...");
    fireEvent.change(textarea, { target: { value: "do the thing" } });
    fireEvent.click(screen.getByRole("button", { name: "Send message" }));

    await waitFor(() => expect(onSend).toHaveBeenCalledWith("do the thing", undefined));
    await waitFor(() => expect(textarea).toHaveValue("do the thing"));
  });

  it("clears the draft after a successful send and blocks duplicate submits", async () => {
    let resolveSend: () => void = () => {};
    const onSend = vi.fn(
      () =>
        new Promise<undefined>((resolve) => {
          resolveSend = () => resolve(undefined);
        }),
    );
    render(<MessageComposer projectId="project-1" onSend={onSend} />);

    const textarea = screen.getByPlaceholderText("Send a message...");
    const sendButton = screen.getByRole("button", { name: "Send message" });
    fireEvent.change(textarea, { target: { value: "ship it" } });
    fireEvent.click(sendButton);
    fireEvent.click(sendButton);

    expect(onSend).toHaveBeenCalledTimes(1);
    resolveSend();
    await waitFor(() => expect(textarea).toHaveValue(""));
  });

  it("does not clobber a fresh draft when a failed send resolves late", async () => {
    let resolveSend: (v: boolean) => void = () => {};
    const onSend = vi.fn(
      () =>
        new Promise<boolean>((resolve) => {
          resolveSend = resolve;
        }),
    );
    render(<MessageComposer projectId="project-1" onSend={onSend} />);

    const textarea = screen.getByPlaceholderText("Send a message...");
    fireEvent.change(textarea, { target: { value: "first" } });
    fireEvent.click(screen.getByRole("button", { name: "Send message" }));

    await waitFor(() => expect(onSend).toHaveBeenCalledWith("first", undefined));
    // The field was cleared optimistically; the user starts a new draft mid-flight.
    await waitFor(() => expect(textarea).toHaveValue(""));
    fireEvent.change(textarea, { target: { value: "second" } });

    resolveSend(false); // send failed — but the old draft must NOT overwrite "second".
    await waitFor(() => expect(textarea).toHaveValue("second"));
  });

  it("captures the pointer on the mic button itself, not the icon child (bug 0.7)", () => {
    render(<MessageComposer projectId="project-1" onSend={vi.fn()} />);

    const micButton = screen.getByRole("button", { name: "Start dictation" });
    const icon = micButton.querySelector("svg");
    expect(icon).not.toBeNull();

    const buttonCapture = vi.fn();
    const iconCapture = vi.fn();
    micButton.setPointerCapture = buttonCapture;
    (icon as unknown as HTMLElement).setPointerCapture = iconCapture;

    // Dispatch on the icon — currentTarget is still the button. The fix must
    // capture on currentTarget (button), so pointerup/cancel reach it and the
    // mic can't get stuck "on".
    fireEvent.pointerDown(icon as Element, { button: 0, pointerId: 7 });

    expect(buttonCapture).toHaveBeenCalled();
    expect(iconCapture).not.toHaveBeenCalled();

    fireEvent.pointerUp(micButton); // release the pending hold timer
  });

  it("routes Enter on an empty composer to onEmptySubmit, not onSend", () => {
    const onSend = vi.fn();
    const onEmptySubmit = vi.fn();
    render(<MessageComposer projectId="project-1" onSend={onSend} onEmptySubmit={onEmptySubmit} />);

    const textarea = screen.getByPlaceholderText("Send a message...");
    fireEvent.keyDown(textarea, { key: "Enter" });

    expect(onEmptySubmit).toHaveBeenCalledTimes(1);
    expect(onSend).not.toHaveBeenCalled();
  });

  it("stashes with Ctrl+S and restores when the field is empty", () => {
    let stashed = "";
    const onStash = vi.fn((t: string) => {
      stashed = t;
    });
    const onUnstash = vi.fn(() => stashed || undefined);
    render(
      <MessageComposer
        projectId="project-1"
        onSend={vi.fn()}
        onStash={onStash}
        onUnstash={onUnstash}
      />,
    );

    const textarea = screen.getByPlaceholderText("Send a message...");
    fireEvent.change(textarea, { target: { value: "draft to stash" } });
    fireEvent.keyDown(textarea, { key: "s", ctrlKey: true });

    expect(onStash).toHaveBeenCalledWith("draft to stash");
    expect(textarea).toHaveValue("");

    fireEvent.keyDown(textarea, { key: "s", ctrlKey: true });
    expect(onUnstash).toHaveBeenCalled();
    expect(textarea).toHaveValue("draft to stash");
  });
});
