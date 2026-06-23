import { chromium } from "playwright-core";
const OUT = process.argv[2] || "/tmp/shot.png";
const SETTLE_MS = Number(process.argv[3] || 16000);
const WHEELS = Number(process.argv[4] || 0);
const browser = await chromium.launch({ headless: true });
const page = await browser.newPage({ viewport: { width: 1456, height: 1100 }, deviceScaleFactor: 1 });
await page.goto("http://127.0.0.1:9210/brain", { waitUntil: "networkidle", timeout: 30000 });
await page.waitForSelector("canvas", { timeout: 20000 }).catch(() => {});
await page.waitForTimeout(SETTLE_MS);
if (WHEELS > 0) {
  await page.mouse.move(800, 470);
  for (let i = 0; i < WHEELS; i++) { await page.mouse.wheel(0, -260); await page.waitForTimeout(120); }
  await page.waitForTimeout(2500);
}
await page.screenshot({ path: OUT });
console.log("shot ->", OUT);
await browser.close();
