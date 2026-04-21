import { describe, expect, it } from "vitest";
import {
  DEFAULT_PRESETS,
  emptyPersonaConfig,
  hydratePersonaConfig,
  stripPersonaConfig,
} from "~/lib/persona-config";
import type { AgentProfileConfig } from "~/lib/team-actions";

describe("emptyPersonaConfig", () => {
  it("returns all fields set to empty strings and default presets", () => {
    const c = emptyPersonaConfig();
    expect(c.model).toBe("");
    expect(c.effort).toBe("");
    expect(c.autoApproveMode).toBe("");
    expect(c.systemPromptAdditions).toBe("");
    expect(c.behaviorPresets).toEqual(DEFAULT_PRESETS);
    expect(c.capabilities).toEqual([]);
  });

  it("returns a fresh presets object each call", () => {
    const a = emptyPersonaConfig();
    const b = emptyPersonaConfig();
    expect(a.behaviorPresets).not.toBe(b.behaviorPresets);
  });
});

describe("hydratePersonaConfig", () => {
  it("returns empty config for undefined input", () => {
    expect(hydratePersonaConfig(undefined)).toEqual(emptyPersonaConfig());
  });

  it("returns empty config for empty object", () => {
    expect(hydratePersonaConfig({})).toEqual(emptyPersonaConfig());
  });

  it("fills missing fields with defaults and keeps set fields", () => {
    const input: AgentProfileConfig = {
      model: "opus",
      behaviorPresets: { ...DEFAULT_PRESETS, planFirst: true },
    };
    const out = hydratePersonaConfig(input);
    expect(out.model).toBe("opus");
    expect(out.effort).toBe("");
    expect(out.behaviorPresets?.planFirst).toBe(true);
    expect(out.behaviorPresets?.autoCommit).toBe(false);
  });

  it("preserves partial behaviorPresets by merging with defaults", () => {
    const input: AgentProfileConfig = {
      behaviorPresets: { autoCommit: true } as never,
    };
    const out = hydratePersonaConfig(input);
    expect(out.behaviorPresets).toEqual({ ...DEFAULT_PRESETS, autoCommit: true });
  });
});

describe("stripPersonaConfig", () => {
  it("drops all empty fields", () => {
    expect(stripPersonaConfig(emptyPersonaConfig())).toEqual({});
  });

  it("keeps non-empty scalar fields", () => {
    const input: AgentProfileConfig = {
      model: "sonnet",
      effort: "high",
      autoApproveMode: "auto",
      behaviorPresets: { ...DEFAULT_PRESETS },
      systemPromptAdditions: "",
    };
    expect(stripPersonaConfig(input)).toEqual({
      model: "sonnet",
      effort: "high",
      autoApproveMode: "auto",
    });
  });

  it("keeps behaviorPresets when any toggle is active", () => {
    const input: AgentProfileConfig = {
      behaviorPresets: { ...DEFAULT_PRESETS, terse: true },
    };
    const out = stripPersonaConfig(input);
    expect(out.behaviorPresets).toEqual({
      autoCommit: false,
      suggestParallel: false,
      planFirst: false,
      terse: true,
    });
  });

  it("keeps behaviorPresets when customInstructions is non-empty", () => {
    const input: AgentProfileConfig = {
      behaviorPresets: { ...DEFAULT_PRESETS, customInstructions: "focus on backend" },
    };
    const out = stripPersonaConfig(input);
    expect(out.behaviorPresets?.customInstructions).toBe("focus on backend");
  });

  it("drops behaviorPresets when all toggles false and customInstructions whitespace-only", () => {
    const input: AgentProfileConfig = {
      behaviorPresets: { ...DEFAULT_PRESETS, customInstructions: "   " },
    };
    expect(stripPersonaConfig(input).behaviorPresets).toBeUndefined();
  });

  it("drops customInstructions key when whitespace-only even with other toggles active", () => {
    const input: AgentProfileConfig = {
      behaviorPresets: { ...DEFAULT_PRESETS, autoCommit: true, customInstructions: "  " },
    };
    const out = stripPersonaConfig(input);
    expect(out.behaviorPresets).toEqual({
      autoCommit: true,
      suggestParallel: false,
      planFirst: false,
      terse: false,
    });
    expect(out.behaviorPresets?.customInstructions).toBeUndefined();
  });

  it("drops systemPromptAdditions when whitespace-only", () => {
    const input: AgentProfileConfig = { systemPromptAdditions: "   \n  " };
    expect(stripPersonaConfig(input)).toEqual({});
  });

  it("keeps systemPromptAdditions when non-empty", () => {
    const input: AgentProfileConfig = { systemPromptAdditions: "You are a backend expert." };
    expect(stripPersonaConfig(input).systemPromptAdditions).toBe("You are a backend expert.");
  });

  it("drops empty capabilities array", () => {
    const input: AgentProfileConfig = { capabilities: [] };
    expect(stripPersonaConfig(input)).toEqual({});
  });

  it("drops capabilities containing only whitespace entries", () => {
    const input: AgentProfileConfig = { capabilities: ["  ", ""] };
    expect(stripPersonaConfig(input).capabilities).toBeUndefined();
  });

  it("trims capabilities and keeps non-empty ones", () => {
    const input: AgentProfileConfig = { capabilities: ["  go-backend ", "react", "  "] };
    expect(stripPersonaConfig(input).capabilities).toEqual(["go-backend", "react"]);
  });

  it("round-trips: strip → hydrate produces equivalent config", () => {
    const input: AgentProfileConfig = {
      model: "opus",
      effort: "xhigh",
      autoApproveMode: "fullAuto",
      behaviorPresets: { ...DEFAULT_PRESETS, planFirst: true, customInstructions: "use TDD" },
      systemPromptAdditions: "Be terse.",
      capabilities: ["go-backend", "sqlc-migrations"],
    };
    const stripped = stripPersonaConfig(input);
    const hydrated = hydratePersonaConfig(stripped);
    expect(hydrated.model).toBe("opus");
    expect(hydrated.effort).toBe("xhigh");
    expect(hydrated.autoApproveMode).toBe("fullAuto");
    expect(hydrated.behaviorPresets?.planFirst).toBe(true);
    expect(hydrated.behaviorPresets?.customInstructions).toBe("use TDD");
    expect(hydrated.systemPromptAdditions).toBe("Be terse.");
    expect(hydrated.capabilities).toEqual(["go-backend", "sqlc-migrations"]);
  });
});
