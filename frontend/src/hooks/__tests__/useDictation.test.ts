/**
 * Routing: the browser's recognizer first, the server where it cannot work,
 * and never a server that is not mounted.
 */
import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const { servers, FakeRecognition, recognizers } = vi.hoisted(() => {
  const recognizers: {
    onerror: ((e: { error: string }) => void) | null;
    onend: (() => void) | null;
  }[] = [];
  class FakeRecognition {
    continuous = false;
    interimResults = false;
    lang = "";
    onresult = null;
    onerror: ((e: { error: string }) => void) | null = null;
    onend: (() => void) | null = null;
    constructor() {
      recognizers.push(this);
    }
    start() {}
    stop() {
      this.onend?.();
    }
    abort() {}
  }
  (globalThis as unknown as { SpeechRecognition: unknown }).SpeechRecognition = FakeRecognition;
  const servers: { handlers: Record<string, (...a: unknown[]) => void>; stop: () => void }[] = [];
  return { servers, FakeRecognition, recognizers };
});

vi.mock("~/lib/speech/server-dictation", () => ({
  dictationUrl: () => "ws://test/api/voice/dictation",
  ServerDictation: class {
    handlers: Record<string, (...a: unknown[]) => void> = {};
    async start(h: Record<string, (...a: unknown[]) => void>) {
      this.handlers = h;
      servers.push(this);
      h.onPhase?.("connecting");
    }
    stop() {
      this.handlers.onEnd?.();
    }
    abort() {}
  },
}));
vi.mock("~/lib/voice/capture", () => ({ MicCapture: class {} }));

async function mount(serverAvailable: boolean) {
  // Fresh modules per test (the fallback is remembered module-wide), so the
  // store has to come from the same module graph as the hook.
  const { useFeatureStore } = await import("~/stores/feature-store");
  useFeatureStore.setState((s) => ({ features: { ...s.features, dictation: serverAvailable } }));
  const onFault = vi.fn();
  const onFallback = vi.fn();
  const onTranscript = vi.fn();
  const mod = await import("../useDictation");
  const hook = renderHook(() => mod.useDictation({ onTranscript, onFault, onFallback }));
  return { ...hook, onFault, onFallback, onTranscript };
}

beforeEach(() => {
  servers.length = 0;
  recognizers.length = 0;
  vi.resetModules();
  vi.spyOn(console, "warn").mockImplementation(() => {});
});
afterEach(() => {
  vi.restoreAllMocks();
});

describe("useDictation routing", () => {
  it("continues the same press on the server when the browser's service never answers", async () => {
    const { result, onFallback, onFault } = await mount(true);
    act(() => result.current.toggle());
    expect(result.current.route).toBe("browser");

    act(() => {
      recognizers[0]?.onerror?.({ error: "network" });
      recognizers[0]?.onend?.();
    });

    expect(onFallback).toHaveBeenCalledWith("service-unreachable");
    expect(onFault).not.toHaveBeenCalled();
    expect(servers).toHaveLength(1);
    expect(result.current.route).toBe("server");
    expect(result.current.isListening).toBe(true);
    expect(result.current.phase).toBe("connecting");
    expect(result.current.fault).toBeNull();
  });

  it("reports the fault when no server dictation is mounted", async () => {
    const { result, onFault } = await mount(false);
    act(() => result.current.toggle());
    act(() => {
      recognizers[0]?.onerror?.({ error: "network" });
      recognizers[0]?.onend?.();
    });
    expect(onFault).toHaveBeenCalledWith("service-unreachable");
    expect(servers).toHaveLength(0);
    expect(result.current.fault).toBe("service-unreachable");
  });

  it("joins server text a sentence at a time", async () => {
    const { result, onTranscript } = await mount(true);
    act(() => result.current.toggle());
    act(() => {
      recognizers.at(-1)?.onerror?.({ error: "service-not-allowed" });
      recognizers.at(-1)?.onend?.();
    });
    const server = servers.at(-1);
    act(() => {
      server?.handlers.onText?.("Refactor the", true);
      server?.handlers.onText?.(" reconnect logic.", false);
      server?.handlers.onText?.("Then add a test.", true);
    });
    expect(onTranscript).toHaveBeenLastCalledWith("Refactor the reconnect logic. Then add a test.");
  });

  it("does not route around a microphone the operator refused", async () => {
    const { result, onFault } = await mount(true);
    act(() => result.current.toggle());
    act(() => {
      recognizers[0]?.onerror?.({ error: "not-allowed" });
      recognizers[0]?.onend?.();
    });
    expect(onFault).toHaveBeenCalledWith("mic-denied");
    expect(servers).toHaveLength(0);
  });
});

void FakeRecognition;
