/**
 * The thread's header: the call and Memory on the band, Policies and the digest
 * behind one ⋯ menu.
 *
 * Memory is the entry that bites: the brain is off by default, and off means the
 * server mounts no `/api/brain` routes — an unmounted `/api/` path falls
 * through to the SPA and answers 200 with HTML, so an entry drawn without the
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
    features: { browser: false, teams: false, dictation: false, assistant: true, ...over },
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

/**
 * Opens the ⋯ menu and hands back its items.
 *
 * Radix opens on `pointerdown` and not on click, which is the one thing a test
 * of a menu has to know.
 */
async function openMenu() {
  fireEvent.pointerDown(
    screen.getByLabelText("Assistant menu"),
    new MouseEvent("pointerdown", { bubbles: true }),
  );
  return await screen.findByRole("menu");
}

describe("AssistantThreadHeader", () => {
  it("puts memory on the band and policies and digest in the menu", async () => {
    features({ brain: true, voice: true });
    render(<AssistantThreadHeader />);
    // The band carries the call, Memory and the trigger.
    expect(screen.queryByRole("menuitem")).toBeNull();
    expect(screen.getByLabelText(/live call/i)).toBeTruthy();
    expect(screen.getByLabelText("Memory")).toHaveAttribute("href", "/assistant/memory");

    await openMenu();
    // One way in from this header: the band's button, not a second menu row.
    expect(screen.queryByRole("menuitem", { name: /memory/i })).toBeNull();
    expect(screen.getByRole("menuitem", { name: /policies/i })).toHaveAttribute(
      "href",
      "/assistant/policies",
    );
    expect(screen.getByRole("menuitem", { name: /digest/i })).toBeTruthy();
  });

  it("draws no memory entry when the brain is off", async () => {
    features({ brain: false, voice: true });
    render(<AssistantThreadHeader />);
    expect(screen.queryByLabelText("Memory")).toBeNull();
    await openMenu();
    // Policies is the core's own and is unaffected, as is the call.
    expect(screen.getByRole("menuitem", { name: /policies/i })).toBeTruthy();
    expect(screen.getByLabelText(/live call/i)).toBeTruthy();
  });

  it("asks for a digest, whatever else is off", async () => {
    features({ brain: false, voice: false });
    render(<AssistantThreadHeader />);
    await openMenu();
    // The digest is the core's own, so it is gated on neither the brain nor
    // voice — it is the one entry that is always there.
    fireEvent.click(screen.getByRole("menuitem", { name: /digest/i }));
    expect(digest).toHaveBeenCalledTimes(1);
  });

  it("keeps memory and the menu when voice is off", async () => {
    features({ brain: true, voice: false });
    render(<AssistantThreadHeader />);
    expect(screen.getByLabelText("Memory")).toBeTruthy();
    await openMenu();
    expect(screen.getByRole("menuitem", { name: /policies/i })).toBeTruthy();
    expect(screen.queryByLabelText(/live call/i)).toBeNull();
  });
});
