import { describe, expect, it } from "vitest";
import {
  type AudioRoute,
  EXTERNAL_LATENCY_SECONDS,
  HANDSFREE_RATE_CEILING,
  NO_ROUTE,
  readDistance,
  readProfile,
  readRoute,
} from "./audio-route";

/** A context with only the properties the reader is allowed to touch. */
function fakeContext(fields: Partial<Record<string, unknown>>): AudioContext {
  return {
    state: "running",
    sampleRate: 48000,
    baseLatency: 0.01,
    currentTime: 1.5,
    ...fields,
  } as unknown as AudioContext;
}

function route(fields: Partial<AudioRoute>): AudioRoute {
  return { ...NO_ROUTE, sampleRate: 48000, outputLatency: 0.02, ...fields };
}

describe("readRoute", () => {
  it("reports no context as its own state rather than as zeroes", () => {
    expect(readRoute(null)).toBe(NO_ROUTE);
    expect(readRoute(null).state).toBe("none");
  });

  it("reads what the browser exposes", () => {
    const read = readRoute(
      fakeContext({ outputLatency: 0.42, sinkId: "car", state: "suspended", currentTime: 9 }),
    );
    expect(read).toEqual({
      state: "suspended",
      sampleRate: 48000,
      baseLatency: 0.01,
      outputLatency: 0.42,
      sinkId: "car",
      clock: 9,
    });
  });

  it("says it does not know rather than reporting zero latency", () => {
    // Reporting 0 for a number the browser withheld would read as "no latency
    // at all", which is the opposite of the finding.
    const read = readRoute(fakeContext({ baseLatency: undefined }));
    expect(read.outputLatency).toBe(-1);
    expect(read.baseLatency).toBe(-1);
  });

  it("ignores the object form of sinkId, which names no device", () => {
    expect(readRoute(fakeContext({ sinkId: { type: "none" } })).sinkId).toBe("");
  });
});

describe("readDistance", () => {
  it("is unknown when the browser withheld the latency", () => {
    expect(readDistance(route({ outputLatency: -1 }))).toBe("unknown");
  });

  it("reads a short path as the handset's own output", () => {
    expect(readDistance(route({ outputLatency: 0.02 }))).toBe("handset");
  });

  it("reads a buffered path as something on the other end of a link", () => {
    expect(readDistance(route({ outputLatency: 0.3 }))).toBe("external");
  });

  it("counts the threshold itself as external", () => {
    expect(readDistance(route({ outputLatency: EXTERNAL_LATENCY_SECONDS }))).toBe("external");
  });
});

describe("readProfile", () => {
  it("is unknown when no output is open", () => {
    expect(readProfile(NO_ROUTE)).toBe("unknown");
  });

  it("reads 48 kHz as a media route", () => {
    expect(readProfile(route({ sampleRate: 48000 }))).toBe("media");
  });

  it("reads wideband and narrowband telephony rates as hands-free", () => {
    expect(readProfile(route({ sampleRate: HANDSFREE_RATE_CEILING }))).toBe("handsfree");
    expect(readProfile(route({ sampleRate: 8000 }))).toBe("handsfree");
  });
});
