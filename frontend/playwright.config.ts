import path from "path";
import { defineConfig } from "@playwright/test";

const isWindows = process.platform === "win32";
const binaryName = isWindows ? "agentique.exe" : "agentique";
const binaryPath = path.resolve(import.meta.dirname, "..", binaryName);

export default defineConfig({
  testDir: "./e2e",
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: 0,
  workers: 1,
  reporter: "list",
  use: {
    baseURL: "http://localhost:8080",
    trace: "on-first-retry",
  },
  webServer: {
    command: `"${binaryPath}" -addr :8080`,
    url: "http://localhost:8080/api/health",
    cwd: path.resolve(import.meta.dirname, ".."),
    reuseExistingServer: !process.env.CI,
    timeout: 15000,
  },
});
