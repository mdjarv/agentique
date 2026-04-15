import { readFileSync } from "node:fs";
import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import type { Scenario, SeedProject, SeedRequest, SeedSession } from "./fixtures";

/**
 * RecordedFixture matches the JSON format produced by
 * GET /api/test/export-session/{id} on the backend.
 */
export interface RecordedFixture {
  metadata: {
    sessionId: string;
    sessionName: string;
    projectName: string;
    projectPath: string;
    model: string;
    capturedAt: string;
  };
  turns: Array<{
    prompt: string;
    scenario: Scenario;
  }>;
}

const FIXTURES_DIR = resolve(dirname(fileURLToPath(import.meta.url)), "../fixtures");

/**
 * Load a recorded fixture by name (without .json extension).
 */
export function loadFixture(name: string): RecordedFixture {
  const filePath = resolve(FIXTURES_DIR, `${name}.json`);
  const raw = readFileSync(filePath, "utf-8");
  return JSON.parse(raw) as RecordedFixture;
}

/**
 * Convert a recorded fixture into a SeedRequest for the /api/test/seed endpoint.
 *
 * Each turn's scenario maps to a behavior entry. The hybrid test backend
 * replays one scenario per Query call, so tests must send prompts in order.
 */
export function fixtureToSeed(
  fixture: RecordedFixture,
  overrides?: {
    projectId?: string;
    sessionId?: string;
    projectSlug?: string;
    planMode?: boolean;
    autoApproveMode?: string;
    /** Cap all event delays to this value (ms). Speeds up slow fixtures. */
    maxDelay?: number;
  },
): SeedRequest {
  const projectId = overrides?.projectId ?? "fix-proj-0000-0000-000000000001";
  const sessionId = overrides?.sessionId ?? "fix-sess-0000-0000-000000000001";
  const slug = overrides?.projectSlug ?? slugify(fixture.metadata.projectName);

  const project: SeedProject = {
    id: projectId,
    name: fixture.metadata.projectName,
    path: fixture.metadata.projectPath,
    slug,
  };

  let behaviors = fixture.turns.map((t) => t.scenario);
  if (overrides?.maxDelay != null) {
    const cap = overrides.maxDelay;
    behaviors = behaviors.map((scenario) => ({
      events: scenario.events.map((se) => ({
        ...se,
        delay: se.delay != null ? Math.min(se.delay, cap) : se.delay,
      })),
    }));
  }

  const session: SeedSession = {
    id: sessionId,
    projectId,
    name: fixture.metadata.sessionName,
    workDir: fixture.metadata.projectPath,
    live: true,
    behavior: behaviors,
    planMode: overrides?.planMode,
    autoApproveMode: overrides?.autoApproveMode ?? "fullAuto",
  };

  return { projects: [project], sessions: [session] };
}

/**
 * Extract prompts from a fixture in turn order.
 * Tests use these to drive the conversation via the composer.
 */
export function fixturePrompts(fixture: RecordedFixture): string[] {
  return fixture.turns.map((t) => t.prompt).filter(Boolean);
}

function slugify(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-|-$/g, "");
}
