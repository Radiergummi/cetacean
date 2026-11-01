import { expect, test } from "@playwright/test";
import { audit } from "./audit.ts";
import { publishedPaths, widths } from "./pages.ts";

for (const path of publishedPaths()) {
  for (const width of widths) {
    test(`layout ${path} @${width}`, async ({ page }) => {
      const errors: string[] = [];

      page.on("pageerror", ({ message }) => errors.push(message));
      page.on("console", (message) => {
        if (message.type() === "error") {
          errors.push(message.text());
        }
      });

      await page.setViewportSize({ width, height: 900 });
      await page.goto(path, { waitUntil: "networkidle" });
      await page.evaluate(() => document.fonts.ready);

      const findings = (await page.evaluate(audit, width)).map(
        ({ rule, selector, detail }) => `${rule} ${selector}: ${detail}`,
      );

      expect.soft(errors, "console errors").toEqual([]);
      expect.soft(findings, "layout findings").toEqual([]);
    });
  }
}
