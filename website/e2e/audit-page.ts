import { chromium, devices, webkit } from "@playwright/test";
import { audit } from "./audit.ts";

const [target = "/", widthArgument = "375", ...flags] = process.argv.slice(2);
const width = Number(widthArgument);
const url = new URL(target, "http://localhost:4321");
const engine = flags.includes("--webkit") ? webkit : chromium;
const browser = await engine.launch();
const context = await browser.newContext(
  flags.includes("--webkit")
    ? { ...devices["iPhone 15"], viewport: { width, height: 900 } }
    : { hasTouch: true, viewport: { width, height: 900 } },
);
const page = await context.newPage();

page.on("pageerror", ({ message }) => console.log(`page error: ${message}`));
await page.goto(url.href, { waitUntil: "networkidle" });
await page.evaluate(() => document.fonts.ready);

const findings = await page.evaluate(audit, width);

for (const { rule, selector, detail } of findings) {
  console.log(`${rule} ${selector}: ${detail}`);
}

console.log(`${findings.length} findings on ${url.pathname} at ${width}px`);
await browser.close();
process.exitCode = findings.length ? 1 : 0;
