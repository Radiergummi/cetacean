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
 * itself, not on something sitting inside it. A bare `row.click()` targets
 * the bounding-box centre, which on `/tasks` lands inside the Node column's
 * link and navigates there instead. So the click goes to the first cell
 * holding no link and no button, which belongs to the row alone.
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
 * Read a JSON document from the API. The dashboard fills itself in after
 * mount, so a spec deciding from the DOM as `goto` resolves reads "absent" for
 * "not fetched yet" — and `test.skip` makes that silent.
 */
export async function apiJson(
  request: import("@playwright/test").APIRequestContext,
  baseURL: string | undefined,
  path: string,
): Promise<Record<string, unknown>> {
  const response = await request.get(`${baseURL}${path}`, {
    headers: { Accept: "application/json" },
  });

  return (await response.json()) as Record<string, unknown>;
}

/** The auth provider the server reports for this run. */
export async function authProvider(
  request: import("@playwright/test").APIRequestContext,
  baseURL: string | undefined,
): Promise<string> {
  const identity = await apiJson(request, baseURL, "/auth/whoami");

  return String(identity.provider ?? "none");
}

/**
 * Whether the SUT holds change history for a resource. It is that process's
 * ring buffer, so a resource unchanged since startup has none however long the
 * dashboard is given — a real precondition, not a wait.
 */
export async function hasHistory(
  request: import("@playwright/test").APIRequestContext,
  baseURL: string | undefined,
  resourceId: string,
): Promise<boolean> {
  const history = await apiJson(
    request,
    baseURL,
    `/history?resourceId=${encodeURIComponent(resourceId)}&limit=1`,
  );
  const items = history.items;

  return Array.isArray(items) && items.length > 0;
}

/**
 * The methods the server offers for a resource, from its `Allow` header — what
 * the dashboard itself gates its write affordances on.
 */
export async function allowedMethods(
  request: import("@playwright/test").APIRequestContext,
  baseURL: string | undefined,
  path: string,
): Promise<Set<string>> {
  const response = await request.get(`${baseURL}${path}`, {
    headers: { Accept: "application/json" },
  });
  const allow = response.headers().allow ?? "";

  return new Set(
    allow
      .split(",")
      .map((method) => method.trim().toUpperCase())
      .filter(Boolean),
  );
}

/** The trailing identifier of the detail page currently open. */
export function detailId(page: import("@playwright/test").Page): string {
  return decodeURIComponent(new URL(page.url()).pathname.split("/").pop() ?? "");
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
