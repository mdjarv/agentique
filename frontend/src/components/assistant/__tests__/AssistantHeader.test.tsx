/**
 * The thread's header carries two controls, and each is gated on the feature
 * behind it.
 *
 * Memory is the one that bites: the brain is off by default, and off means the
 * server mounts no `/api/brain` routes — an unmounted `/api/` path falls
 * through to the SPA and answers 200 with HTML, so a control drawn without the
 * gate leads somewhere that looks alive and is not.
 */
import "@testing-library/jest-dom/vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AssistantThreadHeader } from "~/components/assistant/AssistantHeader";
import { useFeatureStore } from "~/stores/feature-store";
import { useVoiceStore } from "~/stores/voice-store";

const digest = vi.fn();

// No socket is wanted here: the assertion is that the control fires the op.
vi.mock("~/hooks/useWebSocket", () => ({ useWebSocket: () => ({}) }));
vi.mock("~/lib/assistant/rpc", () => ({ digest: (...args: unknown[]) => digest(...args) }));

// Nothing here mounts a router, and the destination is the whole assertion.
vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-router")>();
  return {
    ...actual,
    Link: ({ to, children, ...rest }: { to: string; children?: ReactNode }) => (
      <a href={to} {...rest}>
        {children}
      </a>
    ),
  };
});

function features(over: { brain: boolean; voice: boolean }) {
  useFeatureStore.setState({
    features: { browser: false, teams: false, assistant: true, ...over },
    loaded: true,
  });
}

beforeEach(() => {
  Object.defineProperty(window, "matchMedia", {
    writable: true,
    value: vi.fn().mockImplementation((query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(),
    })),
  });
  useVoiceStore.setState({ status: "idle" } as never);
  digest.mockReset();
  digest.mockResolvedValue(undefined);
});

afterEach(cleanup);

describe("AssistantThreadHeader", () => {
  it("links to memory when the brain is on", () => {
    features({ brain: true, voice: true });
    render(<AssistantThreadHeader />);
    const memory = screen.getByLabelText("Memory");
    expect(memory).toHaveAttribute("href", "/assistant/memory");
  });

  it("draws no memory control when the brain is off", () => {
    features({ brain: false, voice: true });
    render(<AssistantThreadHeader />);
    expect(screen.queryByLabelText("Memory")).toBeNull();
    // The call is a different feature and is unaffected.
    expect(screen.getByLabelText(/live call/i)).toBeTruthy();
  });

  it("asks for a digest, whatever else is off", () => {
    features({ brain: false, voice: false });
    render(<AssistantThreadHeader />);
    // The digest is the core's own, so it is gated on neither the brain nor
    // voice — it is the one control that is always there.
    fireEvent.click(screen.getByLabelText("Digest"));
    expect(digest).toHaveBeenCalledTimes(1);
  });

  it("keeps memory when voice is off", () => {
    features({ brain: true, voice: false });
    render(<AssistantThreadHeader />);
    expect(screen.getByLabelText("Memory")).toBeTruthy();
    expect(screen.queryByLabelText(/live call/i)).toBeNull();
  });
});
