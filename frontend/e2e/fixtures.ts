import { test as base, expect } from "@playwright/test";

interface MonitoringStatus {
  prometheus: boolean;
  nodeExporter: boolean;
  cadvisor: boolean;
}

async function fetchMonitoringStatus(baseURL: string): Promise<MonitoringStatus> {
  try {
    const response = await fetch(`${baseURL}/-/metrics/status`, {
      headers: { Accept: "application/json" },
    });
    const data = await response.json();

    return {
      prometheus: data.prometheusConfigured && data.prometheusReachable,
      nodeExporter: (data.nodeExporter?.targets ?? 0) > 0,
      cadvisor: (data.cadvisor?.targets ?? 0) > 0,
    };
  } catch {
    return { prometheus: false, nodeExporter: false, cadvisor: false };
  }
}

export const test = base.extend<object, { monitoring: MonitoringStatus }>({
  monitoring: [
    // eslint-disable-next-line no-empty-pattern, react-hooks/rules-of-hooks
    async ({}, use, workerInfo) => {
      const baseURL =
        workerInfo.project.use.baseURL ?? process.env.CETACEAN_E2E_URL ?? "http://localhost:9000";
      const status = await fetchMonitoringStatus(baseURL);
      await use(status);
    },
    { scope: "worker" },
  ],
});

export { expect };

/** Whether write operations are enabled for this test run. */
export const writesEnabled = !!process.env.CETACEAN_E2E_WRITE;

/**
 * Navigate to the first item in a resource list and wait for the detail page.
 * Uses `table tbody tr` because DataTable renders standard HTML table elements
 * and Playwright's role-based `getByRole("row")` also matches the header row.
 */
export async function navigateToFirst(
  page: import("@playwright/test").Page,
  listPath: string,
  detailPattern: RegExp,
) {
  await page.goto(listPath);
  const firstRow = page.locator("table tbody tr").first();
  await expect(firstRow).toBeVisible({ timeout: 10_000 });
  await firstRow.click();
  await expect(page).toHaveURL(detailPattern);
}
