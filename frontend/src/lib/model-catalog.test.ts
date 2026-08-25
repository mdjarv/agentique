import { beforeEach, describe, expect, it } from "vitest";
import {
  buildModelOptions,
  DEFAULT_MODEL_FOR_PROVIDER,
  modelLabel,
  resolvedModelLabel,
  sessionModelLabel,
} from "~/lib/model-catalog";
import { useProviderStore } from "~/stores/provider-store";

describe("model catalog", () => {
  beforeEach(() => {
    useProviderStore.setState({ models: {}, sources: {} });
  });

  it("offers one stable Claude choice per family", () => {
    const { options } = buildModelOptions({}, "claude");

    expect(options.map(({ value, label }) => ({ value, label }))).toEqual([
      { value: "haiku", label: "Haiku" },
      { value: "sonnet[1m]", label: "Sonnet" },
      { value: "opus[1m]", label: "Opus" },
      { value: "fable", label: "Fable" },
    ]);
    expect(DEFAULT_MODEL_FOR_PROVIDER.claude).toBe("sonnet[1m]");
  });

  it("keeps legacy aliases readable without putting them back in the picker", () => {
    expect(modelLabel("opus")).toBe("Opus");
    expect(modelLabel("sonnet")).toBe("Sonnet");
  });

  it("shows a concrete version only when a session reports one", () => {
    expect(sessionModelLabel("opus[1m]", undefined)).toBe("Opus");
    expect(sessionModelLabel("opus[1m]", "claude-opus-5[1m]")).toBe("Opus 5");
    expect(resolvedModelLabel("claude-3-5-sonnet-20241022")).toBe("Sonnet 3.5");
    expect(resolvedModelLabel("claude-quartz-6-2")).toBe("Quartz 6.2");
  });
});
