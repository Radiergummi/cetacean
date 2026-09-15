import { test, expect, apiJson } from "./fixtures";

test.describe("Range Request Pagination (API)", () => {
  test("list endpoint returns Accept-Ranges: items", async ({ request, baseURL }) => {
    const response = await request.get(`${baseURL}/nodes`, {
      headers: { Accept: "application/json" },
    });

    expect(response.ok()).toBe(true);
    expect(response.headers()["accept-ranges"]).toBe("items");
  });

  test("Range header returns 206 with Content-Range", async ({ request, baseURL }) => {
    const response = await request.get(`${baseURL}/services`, {
      headers: {
        Accept: "application/json",
        Range: "items 0-1",
      },
    });

    const status = response.status();
    const body = await response.json();

    // 206 if there are more items than requested, 200 if the range covers everything
    if (body.total > 2) {
      expect(status).toBe(206);

      const contentRange = response.headers()["content-range"];
      expect(contentRange).toMatch(/^items 0-1\/\d+$/);
    } else {
      expect(status).toBe(200);
    }

    expect(response.headers()["accept-ranges"]).toBe("items");
    expect(body.items).toBeInstanceOf(Array);
    expect(body.items.length).toBeLessThanOrEqual(2);
    expect(body.total).toBeGreaterThanOrEqual(0);
  });

  test("query params override Range header", async ({ request, baseURL }) => {
    const response = await request.get(`${baseURL}/services?limit=1&offset=0`, {
      headers: {
        Accept: "application/json",
        Range: "items 0-49",
      },
    });

    // Query params take precedence: always 200, no Content-Range
    expect(response.status()).toBe(200);
    expect(response.headers()["content-range"]).toBeUndefined();
    expect(response.headers()["accept-ranges"]).toBe("items");

    const body = await response.json();
    expect(body.items.length).toBeLessThanOrEqual(1);
  });

  test("multipart range returns 416", async ({ request, baseURL }) => {
    const response = await request.get(`${baseURL}/services`, {
      headers: {
        Accept: "application/json",
        Range: "items 0-4, 10-14",
      },
    });

    expect(response.status()).toBe(416);
  });

  test("range beyond total returns 416 with unsatisfied Content-Range", async ({
    request,
    baseURL,
  }) => {
    const response = await request.get(`${baseURL}/services`, {
      headers: {
        Accept: "application/json",
        Range: "items 99999-99999",
      },
    });

    expect(response.status()).toBe(416);

    const contentRange = response.headers()["content-range"];
    expect(contentRange).toMatch(/^items \*\/\d+$/);
  });
});

test.describe("Infinite Scroll (UI)", () => {
  /**
   * The sentinel shows only while a page is outstanding, so this needs a list
   * longer than the dashboard's page size (`pageSize` in api/client.ts). Which
   * list that is depends on the cluster, so each is asked in turn.
   */
  const paginatedLists = ["/tasks", "/configs", "/secrets", "/services", "/networks", "/volumes"];
  const dashboardPageSize = 50;

  test("load-more sentinel appears when list has more items", async ({
    page,
    request,
    baseURL,
  }) => {
    let listPath: string | null = null;

    for (const candidate of paginatedLists) {
      // eslint-disable-next-line no-await-in-loop
      const body = await apiJson(request, baseURL, candidate);

      if (typeof body.total === "number" && body.total > dashboardPageSize) {
        listPath = candidate;
        break;
      }
    }

    test.skip(listPath === null, `No list holds more than ${dashboardPageSize} items`);

    // The next page is requested the moment the sentinel mounts, and against a
    // local server the answer beats the assertion. Holding it open is what
    // makes the sentinel observable.
    await page.route(`**${listPath}*`, async (route) => {
      const range = route.request().headers().range ?? "";

      if (range.startsWith("items 0-")) {
        await route.continue();

        return;
      }

      await new Promise((resolve) => setTimeout(resolve, 3_000));
      await route.continue();
    });

    await page.goto(listPath as string);
    await expect(page.getByRole("grid")).toBeVisible({ timeout: 10_000 });

    // The sentinel row should be present when there are more items to load
    await expect(page.getByTestId("load-more-sentinel")).toBeVisible({ timeout: 5_000 });

    // And gone once they have loaded, which is what it is a sentinel for.
    await expect(page.getByTestId("load-more-sentinel")).toBeHidden({ timeout: 15_000 });
  });
});
