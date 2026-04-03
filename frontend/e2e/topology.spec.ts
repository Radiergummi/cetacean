import { test, expect } from "./fixtures";

test.describe("Topology (/topology)", () => {
  test("renders Topology heading", async ({ page }) => {
    await page.goto("/topology");
    await expect(page.getByRole("heading", { name: "Topology" })).toBeVisible({ timeout: 10_000 });
  });

  test("logical view: ReactFlow canvas is visible", async ({ page }) => {
    await page.goto("/topology");
    await expect(page.getByRole("heading", { name: "Topology" })).toBeVisible({ timeout: 10_000 });
    // ReactFlow renders a div.react-flow — no semantic selector available
    await expect(page.locator(".react-flow")).toBeVisible({ timeout: 10_000 });
  });

  test("physical view: switching segmented control keeps canvas visible", async ({ page }) => {
    await page.goto("/topology");
    await expect(page.getByRole("heading", { name: "Topology" })).toBeVisible({ timeout: 10_000 });

    const physicalButton = page.getByRole("button", { name: "Physical" });
    await expect(physicalButton).toBeVisible({ timeout: 10_000 });
    await physicalButton.click();

    // ReactFlow renders a div.react-flow — no semantic selector available
    await expect(page.locator(".react-flow")).toBeVisible({ timeout: 10_000 });
  });
});

test.describe("Topology Export Formats (API)", () => {
  test("JGF: /topology returns application/vnd.jgf+json with two graphs", async ({
    request,
    baseURL,
  }) => {
    const response = await request.get(`${baseURL}/topology`, {
      headers: { Accept: "application/vnd.jgf+json" },
    });

    expect(response.ok()).toBe(true);
    expect(response.headers()["content-type"]).toContain("application/vnd.jgf+json");

    const body = await response.json();
    expect(body.graphs).toHaveLength(2);
    expect(body.graphs[0].id).toBe("network");
    expect(body.graphs[1].id).toBe("placement");
  });

  test("GraphML: /topology returns valid XML with graphml root", async ({ request, baseURL }) => {
    const response = await request.get(`${baseURL}/topology`, {
      headers: { Accept: "application/graphml+xml" },
    });

    expect(response.ok()).toBe(true);
    expect(response.headers()["content-type"]).toContain("application/graphml+xml");

    const body = await response.text();
    expect(body).toContain("<?xml");
    expect(body).toContain("<graphml");
  });

  test("DOT: /topology returns valid Graphviz format", async ({ request, baseURL }) => {
    const response = await request.get(`${baseURL}/topology`, {
      headers: { Accept: "text/vnd.graphviz" },
    });

    expect(response.ok()).toBe(true);
    expect(response.headers()["content-type"]).toContain("text/vnd.graphviz");

    const body = await response.text();
    expect(body).toMatch(/^graph /);
  });

  test(".jgf extension suffix returns JGF", async ({ request, baseURL }) => {
    const response = await request.get(`${baseURL}/topology.jgf`);

    expect(response.ok()).toBe(true);

    const body = await response.json();
    expect(body.graphs).toBeDefined();
  });

  test(".graphml extension suffix returns GraphML", async ({ request, baseURL }) => {
    const response = await request.get(`${baseURL}/topology.graphml`);

    expect(response.ok()).toBe(true);

    const body = await response.text();
    expect(body).toContain("<graphml");
  });

  test(".dot extension suffix returns DOT", async ({ request, baseURL }) => {
    const response = await request.get(`${baseURL}/topology.dot`);

    expect(response.ok()).toBe(true);

    const body = await response.text();
    expect(body).toMatch(/^graph /);
  });

  test("deprecated /topology/networks includes deprecation headers", async ({
    request,
    baseURL,
  }) => {
    const response = await request.get(`${baseURL}/topology/networks`, {
      headers: { Accept: "application/json" },
    });

    expect(response.ok()).toBe(true);
    expect(response.headers()["deprecation"]).toBe("true");
    expect(response.headers()["link"]).toContain("successor-version");
  });
});
