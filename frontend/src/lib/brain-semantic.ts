/**
 * Where the brain's semantic recall stands (docs/brain.md, the runbook), as one
 * closed table read by both surfaces that report it: the Memory page's badge
 * and the steward's `semantic-recall-down` row. The precedent is
 * `lib/update-mark.ts` — a badge and the row behind a mark must not describe
 * one fact two ways.
 *
 * The server sends `semanticState` beside the older `semantic` boolean
 * (`internal/brain/semantic.go`, `SemanticStatus.Wire`, hand-synced). A peer
 * that predates it sends only the boolean, and that reads as Semantic or
 * Keyword with nothing more said — never as "off", because an absent field is
 * "not reported", not a choice.
 */

import { formatTurnTime } from "~/lib/format";

export type SemanticState = "off" | "connecting" | "on" | "unreachable";

export type SemanticReason =
  | "chroma-unreachable"
  | "embedder-unreachable"
  | "collection-failed"
  | "index-failed";

/** The semantic half of `/api/brain/status` and the `brain.semantic` push. */
export interface SemanticReading {
  semantic: boolean;
  semanticState?: SemanticState | string | null;
  semanticReason?: SemanticReason | string | null;
  semanticDownSince?: string | null;
}

export interface SemanticBadge {
  label: "Semantic" | "Keyword";
  /** `warning` only for a configured backend that is gone: keyword by failure. */
  tone: "on" | "quiet" | "warning";
  title: string;
}

/** What failed, as the start of a sentence. */
export function semanticOutage(reason: string | null | undefined): string {
  switch (reason) {
    case "chroma-unreachable":
      return "Chroma is not answering";
    case "embedder-unreachable":
      return "The embedding service is not answering";
    case "collection-failed":
      return "Chroma answers, but the memory collection would not open";
    case "index-failed":
      return "The vector index could not be brought up to date";
    default:
      return "The vector index is unreachable";
  }
}

function since(downSince: string | null | undefined, now: number): string {
  const ts = downSince ? Date.parse(downSince) : Number.NaN;
  return Number.isNaN(ts) ? "" : ` since ${formatTurnTime(ts, now)}`;
}

/** The consequence, said the same way on the badge and on the finding. */
export const OUTAGE_EFFECT =
  "recall matches keywords only and consolidation is paused. It reconnects on its own.";

export function semanticBadge(r: SemanticReading, now: number = Date.now()): SemanticBadge {
  switch (r.semanticState) {
    case "on":
      return { label: "Semantic", tone: "on", title: "Recall blends embeddings with keywords." };
    case "off":
      return {
        label: "Keyword",
        tone: "quiet",
        title: "No vector index is configured, so recall matches keywords.",
      };
    case "connecting":
      return {
        label: "Keyword",
        tone: "quiet",
        title: "Connecting to the vector index. Recall matches keywords until it attaches.",
      };
    case "unreachable":
      return {
        label: "Keyword",
        tone: "warning",
        title: `${semanticOutage(r.semanticReason)}${since(r.semanticDownSince, now)}, so ${OUTAGE_EFFECT}`,
      };
    default:
      return r.semantic
        ? { label: "Semantic", tone: "on", title: "Recall blends embeddings with keywords." }
        : { label: "Keyword", tone: "quiet", title: "Recall matches keywords." };
  }
}

/** The steward finding's row detail, from its facts (`reason`, `since`). */
export function semanticFindingDetail(
  reason: string | null | undefined,
  downSince: string | null | undefined,
  now: number = Date.now(),
): string {
  return `${semanticOutage(reason)}${since(downSince, now)}, so memory ${OUTAGE_EFFECT}`;
}
