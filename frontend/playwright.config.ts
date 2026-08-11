import fs from "node:fs";
import path from "node:path";
import { defineConfig } from "@playwright/test";

const isWindows = process.platform === "win32";
const binaryName = isWindows ? "agentique.exe" : "agentique";
const binaryPath = path.resolve(import.meta.dirname, "..", binaryName);

// Data dir for the run. Must exist before the server starts — the storage
// endpoint statfs's it and 500s on a missing directory.
const dataDir = path.resolve(import.meta.dirname, "..", "tmp", "e2e-home");
fs.mkdirSync(dataDir, { recursive: true });

export default defineConfig({
  testDir: "./e2e",
  testIgnore: ["**/hybrid/**"],
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: 0,
  workers: 1,
  reporter: "list",
  use: {
    baseURL: "http://localhost:8085",
    trace: "on-first-retry",
  },
  webServer: {
    command: `"${binaryPath}" serve --addr :8085 --test-mode`,
    url: "http://localhost:8085/api/health",
    cwd: path.resolve(import.meta.dirname, ".."),
    reuseExistingServer: false,
    timeout: 15000,
    // Isolate the e2e DB (mirrors playwright-hybrid.config.ts). The backend
    // refuses test-mode against the production DB, but pin an explicit path so
    // runs never depend on cwd/build-type and don't litter the repo root.
    //
    // AGENTIQUE_HOME is what actually isolates the *data dir* — worktrees,
    // session files and the owner stamp all hang off paths.DataDir(), so
    // without it a test-mode run writes into the production data dir and the
    // live server's SweepOrphans reclaims those worktrees mid-run (they have
    // no row in its DB). --test-mode already skips the instance lock and the
    // address probe, so this is the only thing standing between an e2e run
    // and the running service.
    env: {
      ...process.env,
      AGENTIQUE_HOME: dataDir,
      AGENTIQUE_DB: path.resolve(import.meta.dirname, "..", "tmp", "test-e2e.db"),
    },
  },
});
