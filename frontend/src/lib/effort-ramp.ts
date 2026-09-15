/**
 * Where a pointer or a key lands on the effort ramp.
 *
 * The ramp is five stops over its width (`RAMP_LEVELS`), so a position snaps to
 * the nearest stop and a key moves one stop. Unset effort ("Default") has no
 * position, so a key starts it on the first stop rather than stepping from a
 * rung it never occupied.
 */
import { type EffortLevel, RAMP_LEVELS } from "~/lib/composer-constants";

/** The stop nearest `fraction` of the ramp's width, clamped to its ends. */
export function rampLevelAt(fraction: number): EffortLevel {
  const last = RAMP_LEVELS.length - 1;
  const clamped = Number.isFinite(fraction) ? Math.min(1, Math.max(0, fraction)) : 0;
  return RAMP_LEVELS[Math.round(clamped * last)] as EffortLevel;
}

/** One stop from `current` in `direction`, held at the ramp's ends. */
export function stepRampLevel(current: EffortLevel, direction: -1 | 1): EffortLevel {
  const last = RAMP_LEVELS.length - 1;
  const idx = RAMP_LEVELS.indexOf(current);
  if (idx < 0) return RAMP_LEVELS[0] as EffortLevel;
  return RAMP_LEVELS[Math.min(last, Math.max(0, idx + direction))] as EffortLevel;
}
