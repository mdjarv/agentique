import { chromium } from "playwright-core";
const FILE = process.argv[2];
const OUT = process.argv[3];
const browser = await chromium.launch({ headless: true });
const page = await browser.newPage({ viewport: { width: 1380, height: 1000 }, deviceScaleFactor: 1.5 });
page.on("console", (m) => console.log("PAGE", m.type(), m.text()));
page.on("pageerror", (e) => console.log("PAGEERR", e.message));
await page.goto("file://" + FILE, { waitUntil: "networkidle" });
await page.waitForTimeout(800);
const info = await page.evaluate(() => ({
  cards: document.querySelectorAll(".card").length,
  bodyH: document.body.scrollHeight,
  cols: getComputedStyle(document.getElementById("grid")).gridTemplateColumns,
}));
console.log("INFO", JSON.stringify(info));
await page.screenshot({ path: OUT, fullPage: true });
console.log("shot ->", OUT);
await browser.close();
