import { test, expect, apiJson } from "./fixtures";

/**
 * The three routes the rest of the suite never visits: the footer's licenses
 * page, the search palette's full-results page, and the `type` link an RFC
 * 9457 problem document carries.
 */
test.describe("Licenses (/licenses)", () => {
  test("renders a card for a component the embedded SBOM names", async ({
    page,
    request,
    baseURL,
  }) => {
    const licenses = await apiJson(request, baseURL, "/-/licenses");
    const components = Array.isArray(licenses.components)
      ? (licenses.components as Record<string, unknown>[])
      : [];
    // An empty list means the SBOM did not reach the binary.
    expect(components.length).toBeGreaterThan(0);

    const [component = {}] = components;

    await page.goto("/licenses");

    await expect(page.getByRole("heading", { name: "Open-source licenses" })).toBeVisible({
      timeout: 10_000,
    });
    await expect(page.getByText(String(component.name), { exact: true }).first()).toBeVisible({
      timeout: 10_000,
    });
  });
});

test.describe("Search results (/search)", () => {
  test("renders results for a query the API matches", async ({ page, request, baseURL }) => {
    const results = await apiJson(request, baseURL, "/search?q=shop");
    test.skip(
      typeof results.total !== "number" || results.total === 0,
      "Nothing in this cluster matches the probe query",
    );

    await page.goto("/search?q=shop");

    await expect(page.getByRole("heading", { name: "Search" })).toBeVisible({ timeout: 10_000 });
    await expect(page.getByRole("link", { name: /shop/i }).first()).toBeVisible({
      timeout: 10_000,
    });
  });

  test("reports an empty result set rather than an empty page", async ({ page }) => {
    await page.goto("/search?q=zzz-no-such-resource-zzz");

    await expect(page.getByRole("heading", { name: "Search" })).toBeVisible({ timeout: 10_000 });
    await expect(page.getByText(/No results for/i)).toBeVisible({ timeout: 10_000 });
  });
});

test.describe("Error reference (/api/errors/:code)", () => {
  test("a code from the index opens its own page", async ({ page, request, baseURL }) => {
    const index = await apiJson(request, baseURL, "/api/errors");
    const items = Array.isArray(index.items) ? (index.items as Record<string, unknown>[]) : [];
    expect(items.length).toBeGreaterThan(0);

    const [definition = {}] = items;
    const code = String(definition.code);

    await page.goto(`/api/errors/${code}`);

    await expect(page.getByRole("heading", { name: new RegExp(`^${code} —`) })).toBeVisible({
      timeout: 10_000,
    });
  });

  test("an unknown code reports the failure instead of rendering blank", async ({ page }) => {
    await page.goto("/api/errors/ZZZ999");

    await expect(page.getByText(/not found/i).first()).toBeVisible({ timeout: 10_000 });
  });
});
