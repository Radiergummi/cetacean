import { test, expect } from "./fixtures";

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
  test("load-more sentinel appears when list has more items", async ({
    page,
    request,
    baseURL,
  }) => {
    // First check if any resource type has enough items to paginate
    const response = await request.get(`${baseURL}/tasks`, {
      headers: {
        Accept: "application/json",
        Range: "items 0-0",
      },
    });
    const body = await response.json();

    test.skip(body.total <= 50, "Not enough tasks to trigger pagination");

    await page.goto("/tasks");
    await expect(page.getByRole("table")).toBeVisible({ timeout: 10_000 });

    // The sentinel row should be present when there are more items to load
    await expect(page.getByTestId("load-more-sentinel")).toBeVisible({ timeout: 5_000 });
  });
});
