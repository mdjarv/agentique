import "@testing-library/jest-dom/vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MessageComposer } from "~/components/chat/MessageComposer";

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
});
