/**
 * The effort ramp inside the brain menu.
 *
 * It regressed to a drawing of a slider — track and thumb inert, only the 9px
 * labels clickable — so these pin that the track itself takes the pointer and
 * the keys, and that a locked ramp takes neither. The two halves are gated
 * separately, so a session that can change effort but not model still opens
 * the menu onto a live ramp.
 */
import "@testing-library/jest-dom/vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { BrainControl } from "~/components/chat/composer/BrainControl";

afterEach(cleanup);

/** Radix opens on `pointerdown`, not click. */
async function openMenu() {
  fireEvent.pointerDown(
    screen.getByRole("button"),
    new MouseEvent("pointerdown", { bubbles: true }),
  );
  return await screen.findByRole("menu");
}

/** jsdom lays nothing out, so the track gets a 200px box at x=100. */
function layOut(el: Element) {
  vi.spyOn(el, "getBoundingClientRect").mockReturnValue({
    left: 100,
    width: 200,
    top: 0,
    height: 20,
    right: 300,
    bottom: 20,
    x: 100,
    y: 0,
    toJSON: () => ({}),
  });
}

describe("BrainControl effort ramp", () => {
  it("sets the level from where the track is pressed", async () => {
    const onEffortChange = vi.fn();
    render(
      <BrainControl
        model="opus[1m]"
        onModelChange={vi.fn()}
        effort="low"
        onEffortChange={onEffortChange}
      />,
    );
    await openMenu();

    const slider = screen.getByRole("slider", { name: "Effort" });
    const track = slider.lastElementChild as HTMLElement;
    layOut(track);

    fireEvent.pointerDown(track, { button: 0, pointerId: 1, clientX: 250 });
    expect(onEffortChange).toHaveBeenLastCalledWith("xhigh");
  });

  it("follows a drag while the pointer is held", async () => {
    const onEffortChange = vi.fn();
    render(
      <BrainControl
        model="opus[1m]"
        onModelChange={vi.fn()}
        effort="low"
        onEffortChange={onEffortChange}
      />,
    );
    await openMenu();

    const track = screen.getByRole("slider", { name: "Effort" }).lastElementChild as HTMLElement;
    layOut(track);
    // jsdom has no pointer capture; stand in for a held button.
    Object.assign(track, { setPointerCapture: vi.fn(), hasPointerCapture: () => true });

    fireEvent.pointerDown(track, { button: 0, pointerId: 1, clientX: 100 });
    fireEvent.pointerMove(track, { pointerId: 1, clientX: 300 });
    expect(onEffortChange).toHaveBeenLastCalledWith("max");
  });

  it("steps with the arrow keys and stays open", async () => {
    const onEffortChange = vi.fn();
    render(
      <BrainControl
        model="opus[1m]"
        onModelChange={vi.fn()}
        effort="high"
        onEffortChange={onEffortChange}
      />,
    );
    const menu = await openMenu();

    const slider = screen.getByRole("slider", { name: "Effort" });
    expect(slider).toHaveAttribute("aria-valuetext", "High");
    fireEvent.keyDown(slider, { key: "ArrowRight" });
    expect(onEffortChange).toHaveBeenLastCalledWith("xhigh");
    fireEvent.keyDown(slider, { key: "ArrowLeft" });
    expect(onEffortChange).toHaveBeenLastCalledWith("medium");

    fireEvent.click(slider);
    expect(menu).toBeInTheDocument();
  });

  it("draws a locked ramp with nothing to operate, and says why", async () => {
    render(
      <BrainControl
        model="opus[1m]"
        onModelChange={vi.fn()}
        effort="high"
        effortNote="Resume the session to change it."
      />,
    );
    await openMenu();
    expect(screen.queryByRole("slider")).toBeNull();
    expect(screen.getByText("Resume the session to change it.")).toBeInTheDocument();
  });

  it("gives a live session's ramp its cost line", async () => {
    const onEffortChange = vi.fn();
    render(
      <BrainControl
        model="opus[1m]"
        onModelChange={vi.fn()}
        effort="medium"
        onEffortChange={onEffortChange}
        effortNote="Changing it re-caches the conversation."
      />,
    );
    await openMenu();

    const slider = screen.getByRole("slider", { name: "Effort" });
    expect(slider).toHaveAttribute("aria-valuetext", "Medium");
    expect(screen.getByText("Changing it re-caches the conversation.")).toBeInTheDocument();
    fireEvent.keyDown(slider, { key: "ArrowRight" });
    expect(onEffortChange).toHaveBeenLastCalledWith("high");
  });

  it("opens onto a live ramp when only effort can change", async () => {
    const onEffortChange = vi.fn();
    render(
      <BrainControl
        model="gpt-5.5"
        modelDisplayName="GPT-5.5"
        effort="low"
        onEffortChange={onEffortChange}
      />,
    );
    await openMenu();

    // The model is stated, not offered.
    const menu = screen.getByRole("menu");
    expect(menu).toHaveTextContent("GPT-5.5");
    expect(screen.queryAllByRole("menuitem")).toHaveLength(0);
    const slider = screen.getByRole("slider", { name: "Effort" });
    fireEvent.keyDown(slider, { key: "ArrowRight" });
    expect(onEffortChange).toHaveBeenLastCalledWith("medium");
  });

  it("is a plain reading when neither half can change", () => {
    render(
      <BrainControl
        model="gpt-5.5"
        modelDisplayName="GPT-5.5"
        effort="low"
        effortNote="Set when the session was created."
      />,
    );
    expect(screen.queryByRole("button")).toBeNull();
    expect(
      screen.getByTitle("GPT-5.5 · Low effort. Set when the session was created."),
    ).toBeInTheDocument();
  });
});
