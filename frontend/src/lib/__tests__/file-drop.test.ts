import { afterEach, describe, expect, it, vi } from "vitest";
import {
  installFileDropGuard,
  isDropTargetActive,
  registerFileSink,
  subscribeFileDrag,
} from "../file-drop";

const teardown: Array<() => void> = [];

afterEach(() => {
  while (teardown.length > 0) teardown.pop()?.();
});

function guard() {
  teardown.push(installFileDropGuard());
}

function sink(fn: (files: File[]) => void = () => {}) {
  teardown.push(registerFileSink(fn));
  return fn;
}

/**
 * jsdom has no DragEvent, and the handlers only ever read `dataTransfer` and
 * call `preventDefault`, so a plain Event carrying a stub is the whole surface.
 */
function fire(type: string, opts: { types?: string[]; files?: File[] } = {}) {
  const event = new Event(type, { bubbles: true, cancelable: true });
  Object.defineProperty(event, "dataTransfer", {
    value: { types: opts.types ?? ["Files"], files: opts.files ?? [], dropEffect: "" },
  });
  window.dispatchEvent(event);
  return event;
}

const png = () => new File(["x"], "shot.png", { type: "image/png" });

describe("file drop guard", () => {
  it("swallows a file drop so the browser cannot navigate to it", () => {
    guard();
    expect(fire("dragover").defaultPrevented).toBe(true);
    expect(fire("drop", { files: [png()] }).defaultPrevented).toBe(true);
  });

  it("swallows the drop even with nowhere to put it", () => {
    guard();
    // No sink registered — a page with no composer still must not navigate.
    expect(fire("drop", { files: [png()] }).defaultPrevented).toBe(true);
  });

  it("ignores a non-file drag, so an in-app reorder is untouched", () => {
    guard();
    const onFiles = vi.fn();
    const s = sink(onFiles);

    fire("dragenter", { types: ["text/plain"] });
    expect(isDropTargetActive(s)).toBe(false);
    expect(fire("drop", { types: ["text/plain"] }).defaultPrevented).toBe(false);
    expect(onFiles).not.toHaveBeenCalled();
  });

  it("delivers dropped files to the sink", () => {
    guard();
    const onFiles = vi.fn();
    sink(onFiles);
    const file = png();

    fire("drop", { files: [file] });
    expect(onFiles).toHaveBeenCalledWith([file]);
  });

  it("delivers to the most recently registered sink", () => {
    guard();
    const first = vi.fn();
    const second = vi.fn();
    sink(first);
    sink(second);

    fire("drop", { files: [png()] });
    expect(first).not.toHaveBeenCalled();
    expect(second).toHaveBeenCalledTimes(1);
  });

  it("stays dragging while the pointer crosses child elements", () => {
    guard();
    const s = sink();
    const changes = vi.fn();
    teardown.push(subscribeFileDrag(changes));

    fire("dragenter"); // over the pane
    expect(isDropTargetActive(s)).toBe(true);

    // Crossing into a child: enter on the new element arrives before leave on
    // the old one, which is exactly what a naive boolean gets wrong.
    fire("dragenter");
    fire("dragleave");
    expect(isDropTargetActive(s)).toBe(true);

    fire("dragleave"); // out of the window
    expect(isDropTargetActive(s)).toBe(false);
  });

  it("clears the drag state on drop", () => {
    guard();
    const s = sink();
    fire("dragenter");
    fire("drop", { files: [png()] });
    expect(isDropTargetActive(s)).toBe(false);
  });

  it("stops listening once every installer has released", () => {
    const release = installFileDropGuard();
    const second = installFileDropGuard();
    release();
    expect(fire("dragover").defaultPrevented).toBe(true);
    second();
    expect(fire("dragover").defaultPrevented).toBe(false);
  });
});
