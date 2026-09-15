/**
 * These read geometry the browser computes, so they need a Cetacean serving
 * *this* frontend — point `CETACEAN_E2E_URL` at one. A deployed instance on
 * the default :9000 may be running an older build, where they will time out
 * waiting for a canvas that is not there.
 */
import { expect, test } from "./fixtures";
import { measureGraph, openFirst, openGraph, stubTraefikLabels } from "./graph";

test.describe("Traefik graph (service detail)", () => {
  test.beforeEach(async ({ page }) => {
    await stubTraefikLabels(page);
    await openFirst(page, "/services", /\/services\/[^/]+$/);
    await openGraph(page);
  });

  test("draws a node per parsed entity and an edge per reference", async ({ page }) => {
    const graph = await measureGraph(page);

    // 2 entrypoints, 3 routers, 4 middlewares, 2 services + 1 unresolved
    expect(graph.nodes).toBe(12);
    expect(graph.edges).toBe(11);
  });

  test("keeps every edge off the nodes it passes", async ({ page }) => {
    expect((await measureGraph(page)).edgesCrossingNodes).toEqual([]);
  });

  test("keeps the edges off each other", async ({ page }) => {
    expect((await measureGraph(page)).edgeCrossings).toEqual([]);
  });

  test("lands the whole graph inside its frame", async ({ page }) => {
    expect((await measureGraph(page)).outsideFrame).toEqual([]);
  });
});

test.describe("Stack graph (stack detail)", () => {
  test.beforeEach(async ({ page }) => {
    await openFirst(page, "/stacks", /\/stacks\/[^/]+$/);
    await openGraph(page);
  });

  test("keeps every edge off the nodes and off each other", async ({ page }) => {
    const graph = await measureGraph(page);

    expect(graph.nodes).toBeGreaterThan(0);
    expect(graph.edgesCrossingNodes).toEqual([]);
    expect(graph.edgeCrossings).toEqual([]);
    expect(graph.outsideFrame).toEqual([]);
  });

  test("makes every node a link to the resource it stands for", async ({ page }) => {
    const { nodes, links } = await measureGraph(page);

    expect(Object.keys(links)).toHaveLength(nodes);

    for (const href of Object.values(links)) {
      expect(href).toMatch(/^\/(services|networks|configs|secrets|volumes)\/.+/);
    }
  });

  test("a node link opens the resource it names", async ({ page }) => {
    const { links } = await measureGraph(page);
    const [id, href] = Object.entries(links)[0]!;

    await page.locator(`.react-flow__node[data-id="${id}"] a`).click();
    await expect(page).toHaveURL(new RegExp(`${href.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}$`));
  });
});

test.describe("Graph viewport", () => {
  test.beforeEach(async ({ page }) => {
    await stubTraefikLabels(page);
    await openFirst(page, "/services", /\/services\/[^/]+$/);
    await openGraph(page);
  });

  test("offers zoom, reset and full screen", async ({ page }) => {
    await Promise.all(
      ["Zoom in", "Zoom out", "Reset view", "Full screen"].map((name) =>
        expect(page.getByRole("button", { name })).toBeVisible(),
      ),
    );
  });

  test("will not zoom out past the point where the graph fits", async ({ page }) => {
    const zoom = () =>
      page.evaluate(() => {
        const transform =
          document.querySelector<HTMLElement>(".react-flow__viewport")!.style.transform;

        return Number(/scale\(([\d.]+)\)/.exec(transform)?.[1]);
      });

    const fitted = await zoom();

    const zoomOut = page.getByRole("button", { name: "Zoom out" });

    for (let count = 0; count < 12; count++) {
      // oxlint-disable-next-line eslint/no-await-in-loop -- sequential by design
      await zoomOut.click();
    }

    await expect.poll(zoom).toBeGreaterThan(fitted * 0.5);
  });

  test("reset brings a panned-away graph back into frame", async ({ page }) => {
    await page.getByRole("button", { name: "Zoom in" }).click();
    await page.getByRole("button", { name: "Zoom in" }).click();
    await page.getByRole("button", { name: "Reset view" }).click();

    await expect.poll(async () => (await measureGraph(page)).outsideFrame).toEqual([]);
  });
});
