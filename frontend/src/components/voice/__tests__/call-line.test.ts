/**
 * The one line, and who gets it.
 *
 * Both call surfaces render `callLine`, so the priority is tested once, here,
 * rather than twice through two DOM trees. What the DOM does have to prove is
 * that a long line cannot break the layout, and that is `VoiceDock.test.tsx`.
 */
import { describe, expect, it } from "vitest";
import { callLine, orbStateFor } from "~/components/voice/use-call-view";

const quiet = {
  status: "live" as const,
  detail: undefined,
  activityLabel: "",
  interim: null,
  lastSpoken: undefined,
};

describe("callLine", () => {
  it("work outranks everything the call is hearing or has heard", () => {
    const line = callLine({
      ...quiet,
      activityLabel: "Summarizing Live Voice Dialog",
      interim: { source: "you", text: "so what I want is" },
      lastSpoken: "Started that on Live Voice Dialog.",
    });
    expect(line).toEqual({ kind: "activity", text: "Summarizing Live Voice Dialog" });
  });

  it("what is being said right now outranks what was said last", () => {
    const line = callLine({
      ...quiet,
      interim: { source: "you", text: "so what I want is" },
      lastSpoken: "Started that on Live Voice Dialog.",
    });
    expect(line).toEqual({ kind: "interim", text: "so what I want is", source: "you" });
  });

  it("an empty interim is not a line — it is the recogniser clearing itself", () => {
    const line = callLine({
      ...quiet,
      interim: { source: "you", text: "" },
      lastSpoken: "Started that.",
    });
    expect(line).toEqual({ kind: "spoken", text: "Started that." });
  });

  it("falls back to the last settled line", () => {
    expect(callLine({ ...quiet, lastSpoken: "Started that." })).toEqual({
      kind: "spoken",
      text: "Started that.",
    });
  });

  it("and to the status when nothing has been said at all", () => {
    expect(callLine(quiet)).toEqual({ kind: "status", text: "Listening" });
  });

  it("a call that is over or not yet up says only that", () => {
    const busy = { ...quiet, activityLabel: "Summarizing", lastSpoken: "hello" };
    expect(callLine({ ...busy, status: "connecting" })).toEqual({
      kind: "status",
      text: "Connecting",
    });
    expect(callLine({ ...busy, status: "ended", detail: "idle" })).toEqual({
      kind: "status",
      text: "Hung up after silence",
    });
    expect(callLine({ ...busy, status: "error", detail: "microphone refused" })).toEqual({
      kind: "status",
      text: "Microphone refused",
    });
  });
});

describe("orbStateFor", () => {
  it("live plus work of its own is working", () => {
    expect(orbStateFor("live", "Summarizing")).toBe("working");
    expect(orbStateFor("live", "")).toBe("live");
  });

  it("both ways a call can be over draw the same empty halo", () => {
    expect(orbStateFor("ended", "")).toBe("ended");
    expect(orbStateFor("error", "")).toBe("ended");
  });

  it("idle and connecting are themselves", () => {
    expect(orbStateFor("idle", "")).toBe("idle");
    expect(orbStateFor("connecting", "")).toBe("connecting");
  });
});
