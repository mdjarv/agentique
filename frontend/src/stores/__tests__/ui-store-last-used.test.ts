import { beforeEach, describe, expect, it } from "vitest";
import { DEFAULT_SESSION_DEFAULTS, useUIStore } from "~/stores/ui-store";

beforeEach(() => {
  useUIStore.setState({
    lastUsed: { model: DEFAULT_SESSION_DEFAULTS.model, effort: DEFAULT_SESSION_DEFAULTS.effort },
  });
});

describe("lastUsed", () => {
  it("starts at the built-in defaults, so a first-ever panel is not blank", () => {
    expect(useUIStore.getState().lastUsed).toEqual({
      model: DEFAULT_SESSION_DEFAULTS.model,
      effort: DEFAULT_SESSION_DEFAULTS.effort,
    });
  });

  it("records what a created session used", () => {
    useUIStore.getState().recordLastUsed({ model: "sonnet", effort: "medium" });
    expect(useUIStore.getState().lastUsed).toEqual({ model: "sonnet", effort: "medium" });
  });

  // Every send calls this, and most sends repeat the previous choice. A new
  // object each time would write localStorage and wake every subscriber for a
  // change that is not one.
  it("is a no-op when nothing changed", () => {
    const before = useUIStore.getState().lastUsed;
    useUIStore.getState().recordLastUsed({ ...before });
    expect(useUIStore.getState().lastUsed).toBe(before);
  });

  it("treats a partial repeat as a change", () => {
    const before = useUIStore.getState().lastUsed;
    useUIStore.getState().recordLastUsed({ model: before.model, effort: "low" });
    expect(useUIStore.getState().lastUsed).not.toBe(before);
    expect(useUIStore.getState().lastUsed.effort).toBe("low");
  });
});
