import type { BehaviorPresets } from "~/lib/generated-types";
import type { AgentProfileConfig } from "~/lib/team-actions";

export const DEFAULT_PRESETS: BehaviorPresets = {
  autoCommit: false,
  suggestParallel: false,
  planFirst: false,
  terse: false,
};

export function emptyPersonaConfig(): AgentProfileConfig {
  return {
    model: "",
    effort: "",
    autoApproveMode: "",
    behaviorPresets: { ...DEFAULT_PRESETS },
    systemPromptAdditions: "",
  };
}

/** Fill a partial config with empty defaults so every field is defined. */
export function hydratePersonaConfig(c: AgentProfileConfig | undefined): AgentProfileConfig {
  const base = emptyPersonaConfig();
  if (!c) return base;
  return {
    model: c.model ?? "",
    effort: c.effort ?? "",
    autoApproveMode: c.autoApproveMode ?? "",
    behaviorPresets: { ...DEFAULT_PRESETS, ...(c.behaviorPresets ?? {}) },
    systemPromptAdditions: c.systemPromptAdditions ?? "",
  };
}

/** Drop empty/default fields so the serialized JSON only carries meaningful overrides. */
export function stripPersonaConfig(c: AgentProfileConfig): AgentProfileConfig {
  const out: AgentProfileConfig = {};
  if (c.model) out.model = c.model;
  if (c.effort) out.effort = c.effort;
  if (c.autoApproveMode) out.autoApproveMode = c.autoApproveMode;
  const bp = c.behaviorPresets;
  if (
    bp &&
    (bp.autoCommit ||
      bp.suggestParallel ||
      bp.planFirst ||
      bp.terse ||
      bp.customInstructions?.trim())
  ) {
    out.behaviorPresets = {
      autoCommit: bp.autoCommit,
      suggestParallel: bp.suggestParallel,
      planFirst: bp.planFirst,
      terse: bp.terse,
      ...(bp.customInstructions?.trim() ? { customInstructions: bp.customInstructions } : {}),
    };
  }
  if (c.systemPromptAdditions?.trim()) out.systemPromptAdditions = c.systemPromptAdditions;
  return out;
}
