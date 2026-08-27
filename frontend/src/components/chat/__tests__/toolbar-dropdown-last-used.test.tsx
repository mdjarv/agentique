import "@testing-library/jest-dom/vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ToolbarDropdown, type ToolbarDropdownOption } from "~/components/chat/ToolbarDropdown";

const OPTIONS: ToolbarDropdownOption[] = [
  { value: "opus[1m]", label: "Opus" },
  { value: "sonnet", label: "Sonnet" },
  { value: "haiku", label: "Haiku" },
];

afterEach(cleanup);

function open(props: Partial<Parameters<typeof ToolbarDropdown>[0]> = {}) {
  render(<ToolbarDropdown value="opus[1m]" onChange={vi.fn()} options={OPTIONS} {...props} />);
  fireEvent.pointerDown(
    screen.getByRole("button"),
    new PointerEvent("pointerdown", { bubbles: true, ctrlKey: false, button: 0 }),
  );
}

describe("ToolbarDropdown last-used marker", () => {
  it("marks the remembered option once the selection has moved off it", () => {
    open({ value: "sonnet", lastUsedValue: "opus[1m]" });
    const marks = screen.getAllByText("last used");
    expect(marks).toHaveLength(1);
    // The mark belongs to Opus, not to the currently-selected Sonnet.
    expect(marks[0]?.closest("[role='menuitem']")).toHaveTextContent("Opus");
  });

  // On open the two coincide, and a row wearing both a tick and a note about
  // itself reports one fact twice.
  it("stays silent while the remembered option is the selected one", () => {
    open({ value: "opus[1m]", lastUsedValue: "opus[1m]" });
    expect(screen.queryByText("last used")).not.toBeInTheDocument();
  });

  // A ModelId is a bare string, so a remembered model can name something this
  // build no longer offers. There is no row to mark, and nothing to say.
  it("stays silent when the remembered value is not in the catalog", () => {
    open({ value: "sonnet", lastUsedValue: "opus-from-a-past-release" });
    expect(screen.queryByText("last used")).not.toBeInTheDocument();
  });

  it("says nothing at all when nothing is remembered", () => {
    open({ value: "sonnet" });
    expect(screen.queryByText("last used")).not.toBeInTheDocument();
  });
});
