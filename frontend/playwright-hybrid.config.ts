import path from "path";
import { defineConfig } from "@playwright/test";

// Hybrid E2E config: real backend in test mode (mock CLI), real SQLite,
// real state machine — only the Claude CLI subprocess is mocked.
// Requires `just build` first (same as regular e2e).

const isWindows = process.platform === "win32";
const binaryName = isWindows ? "agentique.exe" : "agentique";
const binaryPath = path.resolve(import.meta.dirname, "..", binaryName);

export default defineConfig({
  testDir: "./e2e/hybrid",
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: 0,
  workers: 1,
  reporter: "list",
  use: {
    baseURL: "http://localhost:8090",
    trace: "on-first-retry",
  },
  webServer: {
    command: `"${binaryPath}" serve --addr :8090 --test-mode`,
    url: "http://localhost:8090/api/health",
    cwd: path.resolve(import.meta.dirname, ".."),
    reuseExistingServer: !process.env.CI,
    timeout: 15000,
  },
});
