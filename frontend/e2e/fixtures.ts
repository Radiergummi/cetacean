import { test as base, expect } from "@playwright/test";

interface MonitoringStatus {
  prometheus: boolean;
  nodeExporter: boolean;
  cadvisor: boolean;
}

async function fetchMonitoringStatus(baseURL: string): Promise<MonitoringStatus> {
  try {
    const response = await fetch(`${baseURL}/metrics/status`, {
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
 * Click a list row the way a person clicking "the row" does — on the row
 * itself, not on something sitting inside it.
 *
 * A bare `row.click()` targets the row's bounding-box centre, and list rows
 * carry links to related resources: on `/tasks` the centre lands inside the
 * Node column's hostname link, so the click navigates to that node instead of
 * the row's own detail page. That is the links working as intended — they call
 * `stopPropagation` precisely so a link click beats the row click — but it
 * means the centre point is not a measurement of the row's own handler.
 *
 * So the click goes to the first cell holding no link and no button, which
 * belongs to the row alone and bubbles to its handler.
 */
export async function clickRow(row: import("@playwright/test").Locator) {
  const inert = row.locator("td:not(:has(a)):not(:has(button))").first();

  if ((await inert.count()) > 0) {
    await inert.click();

    return;
  }

  await row.click();
}

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
  await clickRow(firstRow);
  await expect(page).toHaveURL(detailPattern);
}
