import { describe, expect, it } from "vitest";
import { type AudioDevice, type AudioRoute, NO_ROUTE } from "./audio-route";
import { type CaptureRoute, NO_CAPTURE } from "./capture";
import { judgeMicRoute, pickBluetoothInput } from "./mic-route";

const DEFAULT: AudioDevice = { id: "default", label: "Default" };
const SPEAKERPHONE: AudioDevice = { id: "sp", label: "Speakerphone" };
const BLUETOOTH: AudioDevice = { id: "bt", label: "Bluetooth headset" };

function capture(over: Partial<CaptureRoute>): CaptureRoute {
  return {
    ...NO_CAPTURE,
    active: true,
    device: "Default",
    deviceId: "default",
    contextSampleRate: 48000,
    uploadSampleRate: 16000,
    ...over,
  };
}

/** An output route by its latency, which is what names the distance. */
function output(outputLatency: number, sampleRate = 48000): AudioRoute {
  return { ...NO_ROUTE, state: "running", sampleRate, outputLatency };
}

describe("pickBluetoothInput", () => {
  it("finds the Bluetooth input by its platform label, whatever the case", () => {
    expect(pickBluetoothInput([DEFAULT, SPEAKERPHONE, BLUETOOTH])).toBe(BLUETOOTH);
    expect(pickBluetoothInput([{ id: "x", label: "BLUETOOTH SCO" }])?.id).toBe("x");
  });

  // Before a permission is granted every label is empty, so a first look
  // cannot name the car — which is why the call looks again after opening.
  it("finds nothing in an unlabelled list", () => {
    expect(
      pickBluetoothInput([
        { id: "a", label: "" },
        { id: "b", label: "" },
      ]),
    ).toBeNull();
    expect(pickBluetoothInput([])).toBeNull();
  });
});

describe("judgeMicRoute", () => {
  it("has nothing to say about a microphone that is not open", () => {
    const verdict = judgeMicRoute({
      devices: [BLUETOOTH],
      capture: { ...NO_CAPTURE },
      playback: output(0.3),
    });
    expect(verdict).toEqual({ action: "ok" });
  });

  // No Bluetooth input means no car to connect to: a handset on its own
  // microphone at a media rate is the correct outcome, not a fault.
  it("accepts the handset's microphone when no Bluetooth input exists", () => {
    const verdict = judgeMicRoute({
      devices: [DEFAULT, SPEAKERPHONE],
      capture: capture({}),
      playback: output(0.3),
    });
    expect(verdict).toEqual({ action: "ok" });
  });

  // The intermittent car fault: default resolution picked the handset while
  // the car's microphone was there to be asked for.
  it("asks for the Bluetooth input when the track opened is not it", () => {
    const verdict = judgeMicRoute({
      devices: [DEFAULT, BLUETOOTH],
      capture: capture({ device: "Default", deviceId: "default" }),
      playback: NO_ROUTE,
    });
    expect(verdict).toEqual({
      action: "retry-with-device",
      device: BLUETOOTH,
      reason: "not-bluetooth",
    });
  });

  it("recognises the Bluetooth track by id or by label", () => {
    expect(
      judgeMicRoute({
        devices: [BLUETOOTH],
        capture: capture({ device: "", deviceId: "bt", contextSampleRate: 16000 }),
        playback: output(0.3, 16000),
      }),
    ).toEqual({ action: "ok" });
    expect(
      judgeMicRoute({
        devices: [BLUETOOTH],
        capture: capture({ device: "Bluetooth headset", deviceId: "", contextSampleRate: 16000 }),
        playback: output(0.3, 16000),
      }),
    ).toEqual({ action: "ok" });
  });

  // The link never came up: the track is the Bluetooth one, but the capture
  // context is on a media rate and the output is going out over a link. HFP
  // would have put the context at 8 or 16 kHz.
  it("asks again when the Bluetooth track came up at a media rate on an external route", () => {
    const verdict = judgeMicRoute({
      devices: [BLUETOOTH],
      capture: capture({ device: "Bluetooth headset", deviceId: "bt", contextSampleRate: 48000 }),
      playback: output(0.3),
    });
    expect(verdict).toEqual({
      action: "retry-with-device",
      device: BLUETOOTH,
      reason: "media-rate",
    });
  });

  // Inconclusive is not wrong. An output on the handset's own speaker means
  // there is no link to have failed, and an unreported latency says nothing.
  it("lets a media-rate Bluetooth track stand when the output is the handset's or unknown", () => {
    const track = capture({
      device: "Bluetooth headset",
      deviceId: "bt",
      contextSampleRate: 48000,
    });
    expect(judgeMicRoute({ devices: [BLUETOOTH], capture: track, playback: output(0.02) })).toEqual(
      {
        action: "ok",
      },
    );
    expect(judgeMicRoute({ devices: [BLUETOOTH], capture: track, playback: NO_ROUTE })).toEqual({
      action: "ok",
    });
  });

  it("is satisfied by the Bluetooth track on a hands-free rate", () => {
    const verdict = judgeMicRoute({
      devices: [DEFAULT, BLUETOOTH],
      capture: capture({ device: "Bluetooth headset", deviceId: "bt", contextSampleRate: 16000 }),
      playback: output(0.3, 16000),
    });
    expect(verdict).toEqual({ action: "ok" });
  });
});
