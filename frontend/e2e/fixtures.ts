import { test as base, expect } from "@playwright/test";

/**
 * Detect whether Prometheus metrics are available by checking the monitoring
 * status endpoint. Caches the result per worker.
 */
let monitoringStatus: { prometheus: boolean; nodeExporter: boolean; cadvisor: boolean } | null = null;

async function getMonitoringStatus(baseURL: string) {
  if (monitoringStatus) {
    return monitoringStatus;
  }

  try {
    const response = await fetch(`${baseURL}/-/metrics/status`, {
      headers: { Accept: "application/json" },
    });
    const data = await response.json();
    monitoringStatus = {
      prometheus: data.prometheusConfigured && data.prometheusReachable,
      nodeExporter: (data.nodeExporter?.targets ?? 0) > 0,
      cadvisor: (data.cadvisor?.targets ?? 0) > 0,
    };
  } catch {
    monitoringStatus = { prometheus: false, nodeExporter: false, cadvisor: false };
  }

  return monitoringStatus;
}

export const test = base.extend<{ monitoring: typeof monitoringStatus }>({
  monitoring: async ({ baseURL }, use) => {
    const status = await getMonitoringStatus(baseURL!);
    await use(status);
  },
});

export { expect };

/** Whether write operations are enabled for this test run. */
export const writesEnabled = !!process.env.CETACEAN_E2E_WRITE;

/** Navigate to the first item in a resource list and wait for the detail page. */
export async function navigateToFirst(
  page: import("@playwright/test").Page,
  listPath: string,
  detailPattern: RegExp,
) {
  await page.goto(listPath);
  await expect(page.locator("table tbody tr").first()).toBeVisible({ timeout: 10_000 });
  await page.locator("table tbody tr").first().click();
  await expect(page).toHaveURL(detailPattern);
}
