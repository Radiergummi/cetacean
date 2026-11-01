import { defineConfig, devices } from "@playwright/test";

const port = 4329;

export default defineConfig({
  testDir: "./e2e",
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  reporter: process.env.CI ? [["github"], ["html", { open: "never" }]] : "list",
  use: {
    baseURL: `http://localhost:${port}`,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  projects: [
    { name: "chromium", use: { browserName: "chromium", hasTouch: true } },
    // The closest automatable stand-in for iOS Safari; local only, by choice.
    ...(process.env.CI ? [] : [{ name: "webkit", use: { ...devices["iPhone 15"] } }]),
  ],
  webServer: {
    command: `node e2e/serve.ts ${port}`,
    port,
    reuseExistingServer: !process.env.CI,
  },
});
