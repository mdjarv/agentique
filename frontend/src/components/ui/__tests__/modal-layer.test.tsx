import "@testing-library/jest-dom/vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Dialog, DialogContent, DialogTitle } from "~/components/ui/dialog";
import { useInModalLayer } from "~/components/ui/modal-layer";
import { Popover, PopoverContent, PopoverTrigger } from "~/components/ui/popover";
import { Sheet, SheetContent, SheetTitle } from "~/components/ui/sheet";

/** jsdom has none of these; Radix's positioning and scroll lock reach for them. */
function installBrowserMocks() {
  window.matchMedia ??= ((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    addListener: vi.fn(),
    removeListener: vi.fn(),
    dispatchEvent: vi.fn(),
  })) as unknown as typeof window.matchMedia;
  globalThis.ResizeObserver ??= class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
  Element.prototype.scrollIntoView ??= vi.fn();
  Element.prototype.hasPointerCapture ??= () => false;
}

function Probe() {
  return <span data-testid="probe">{String(useInModalLayer())}</span>;
}

function Palette({ modal }: { modal?: boolean }) {
  return (
    <Popover modal={modal}>
      <PopoverTrigger asChild>
        <button type="button">open</button>
      </PopoverTrigger>
      <PopoverContent>rows</PopoverContent>
    </Popover>
  );
}

/** The `body > *` branch an element sits in — what `hideOthers` marks. */
function bodyBranchOf(el: Element): Element {
  let node = el;
  while (node.parentElement && node.parentElement !== document.body) node = node.parentElement;
  return node;
}

function sheetHiddenFromAT(): boolean {
  const sheet = document.querySelector('[data-slot="sheet-content"]');
  if (!sheet) throw new Error("sheet not rendered");
  return bodyBranchOf(sheet).getAttribute("aria-hidden") === "true";
}

describe("modal layer", () => {
  beforeEach(installBrowserMocks);
  afterEach(cleanup);

  it("marks a Sheet's subtree", () => {
    render(
      <Sheet open>
        <SheetContent>
          <SheetTitle>nav</SheetTitle>
          <Probe />
        </SheetContent>
      </Sheet>,
    );
    expect(screen.getByTestId("probe")).toHaveTextContent("true");
  });

  it("marks a Dialog's subtree", () => {
    render(
      <Dialog open>
        <DialogContent>
          <DialogTitle>settings</DialogTitle>
          <Probe />
        </DialogContent>
      </Dialog>,
    );
    expect(screen.getByTestId("probe")).toHaveTextContent("true");
  });

  it("leaves an ordinary subtree alone", () => {
    render(<Probe />);
    expect(screen.getByTestId("probe")).toHaveTextContent("false");
  });

  // The reason any of this exists: a popover portalled out of a scroll-locked
  // Sheet is outside the lock's allow-list, so react-remove-scroll cancels
  // every touchmove over its list and the palette stops scrolling on a phone.
  // Taking over as the innermost modal layer is what buys the scroll back.
  it("hands a popover inside a Sheet the modal layer", () => {
    render(
      <Sheet open>
        <SheetContent>
          <SheetTitle>nav</SheetTitle>
          <Palette />
        </SheetContent>
      </Sheet>,
    );
    expect(sheetHiddenFromAT()).toBe(false);
    fireEvent.click(screen.getByText("open"));
    expect(screen.getByText("rows")).toBeInTheDocument();
    expect(sheetHiddenFromAT()).toBe(true);
  });

  it("lets a caller opt out", () => {
    render(
      <Sheet open>
        <SheetContent>
          <SheetTitle>nav</SheetTitle>
          <Palette modal={false} />
        </SheetContent>
      </Sheet>,
    );
    fireEvent.click(screen.getByText("open"));
    expect(screen.getByText("rows")).toBeInTheDocument();
    expect(sheetHiddenFromAT()).toBe(false);
  });

  it("leaves a popover outside any modal layer non-modal", () => {
    render(
      <>
        <div data-slot="sheet-content">not a real sheet</div>
        <Palette />
      </>,
    );
    fireEvent.click(screen.getByText("open"));
    expect(sheetHiddenFromAT()).toBe(false);
  });
});
