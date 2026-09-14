import { describe, expect, it } from "vitest";
import { faultForError, type ProbeNavigator, probeDictationFault } from "./dictation-fault";

function permission(state: PermissionState): PermissionStatus {
  return { state, onchange: null } as unknown as PermissionStatus;
}

describe("faultForError", () => {
  it("reads network as a fault only before anything was heard", () => {
    expect(faultForError("network", false)).toBe("service-unreachable");
    expect(faultForError("network", true)).toBeNull();
  });

  it("names the refusals", () => {
    expect(faultForError("not-allowed", false)).toBe("mic-denied");
    expect(faultForError("service-not-allowed", true)).toBe("service-disabled");
    expect(faultForError("audio-capture", false)).toBe("no-microphone");
  });

  it("is silent about errors that are not faults", () => {
    expect(faultForError("no-speech", false)).toBeNull();
    expect(faultForError("aborted", false)).toBeNull();
  });
});

describe("probeDictationFault", () => {
  it("knows Brave before a click", async () => {
    const nav: ProbeNavigator = { brave: { isBrave: () => Promise.resolve(true) } };
    expect(await probeDictationFault(nav)).toBe("browser-blocked");
  });

  it("knows a denied microphone, and hears it granted later", async () => {
    const status = permission("denied");
    const nav: ProbeNavigator = { permissions: { query: () => Promise.resolve(status) } };
    const changes: unknown[] = [];
    expect(await probeDictationFault(nav, (f) => changes.push(f))).toBe("mic-denied");

    (status as { state: PermissionState }).state = "granted";
    status.onchange?.(new Event("change"));
    expect(changes).toEqual([null]);
  });

  it("fails open when the browser cannot answer", async () => {
    expect(await probeDictationFault({})).toBeNull();
    const nav: ProbeNavigator = {
      brave: { isBrave: () => Promise.reject(new Error("nope")) },
      permissions: { query: () => Promise.reject(new TypeError("unknown name")) },
    };
    expect(await probeDictationFault(nav)).toBeNull();
    expect(
      await probeDictationFault({
        permissions: { query: () => Promise.resolve(permission("prompt")) },
      }),
    ).toBeNull();
  });
});
