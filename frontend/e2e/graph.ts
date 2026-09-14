import { expect } from "./fixtures";
import type { Locator, Page } from "@playwright/test";

/** Press a control repeatedly, each press landing before the next. */
export async function press(control: Locator, times: number) {
  for (let count = 0; count < times; count++) {
    // oxlint-disable-next-line eslint/no-await-in-loop -- sequential by design
    await control.click();
  }
}

/**
 * Open the first resource in a list by its link rather than its row: the row's
 * click handler needs React attached, which is a race a list this simple does
 * not need to run.
 */
export async function openFirst(page: Page, list: string, detail: RegExp) {
  await page.goto(list);

  const link = page.locator(`table tbody tr a[href^="${list}/"]`).first();

  await expect(link).toBeVisible({ timeout: 15_000 });
  await link.click();
  await expect(page).toHaveURL(detail, { timeout: 15_000 });
}

/**
 * The routing a service's Traefik labels would describe, shaped to exercise
 * what the layout has to get right: a chain several steps long, an edge that
 * skips those steps, a reference the labels do not define, a middleware
 * nothing uses, and a router whose service cannot be resolved.
 */
const traefikIntegration = {
  name: "traefik",
  enabled: true,
  routers: [
    {
      name: "shop",
      rule: "Host(`shop.example.com`)",
      entrypoints: ["websecure"],
      middlewares: ["redirect-to-https", "compress", "auth@file"],
      service: "shop",
      priority: 10,
      tls: { certResolver: "letsencrypt" },
    },
    { name: "legacy", rule: "Host(`old.example.com`)", entrypoints: ["web", "websecure"] },
    {
      name: "api",
      rule: "Host(`api.example.com`)",
      entrypoints: ["websecure"],
      middlewares: ["compress"],
      service: "internal",
    },
  ],
  services: [
    { name: "shop", port: 80, scheme: "http" },
    { name: "internal", port: 8081 },
  ],
  middlewares: [
    { name: "compress", type: "compress", config: { minresponsebodybytes: "1024" } },
    { name: "redirect-to-https", type: "redirectscheme", config: { scheme: "https" } },
    { name: "unused", type: "headers", config: { "customrequestheaders.X-Trace": "on" } },
  ],
};

/**
 * Give whichever service the test opens a known set of Traefik labels.
 *
 * The cluster a run points at is not ours to arrange, and the graph is drawn
 * from the parsed integration alone — `internal/integrations` is what tests
 * the parse, so serving a fixed one here pins the layout without pinning the
 * suite to a particular deployment.
 */
export async function stubTraefikLabels(page: Page) {
  await page.route(/\/services\/[^/?#]+(\?.*)?$/, async (route) => {
    // The same URL also serves the SSE stream and, on a hard load, the SPA
    // document. Fetching a stream here never returns, so only the JSON the
    // detail page reads is worth touching.
    if (!(route.request().headers()["accept"] ?? "").includes("json")) {
      await route.continue();

      return;
    }

    const response = await route.fetch();

    await route.fulfill({
      response,
      json: { ...(await response.json()), integrations: [traefikIntegration] },
    });
  });
}

/** Scroll the graph into view and wait for ELK to have placed it. */
export async function openGraph(page: Page) {
  await page.getByTestId("graph-frame").first().scrollIntoViewIfNeeded();

  // React Flow renders a div.react-flow — no semantic selector available. It
  // stays transparent until the layout lands, which is the signal to measure.
  const canvas = page.locator(".react-flow").first();

  await expect(canvas).toBeVisible({ timeout: 20_000 });
  await expect(canvas).toHaveCSS("opacity", "1", { timeout: 20_000 });

  // The fit runs in the effect after the layout commits, so the graph is
  // visible a frame before it is framed. Settle for two identical samples.
  let previous = "";

  await expect
    .poll(
      async () => {
        const current = await page
          .locator(".react-flow__viewport")
          .first()
          .evaluate((element) => element.style.transform);
        const settled = current !== "" && current === previous;

        previous = current;

        return settled;
      },
      { timeout: 20_000, intervals: [100] },
    )
    .toBe(true);
}

export interface GraphGeometry {
  nodes: number;
  edges: number;
  /** Edge ids whose path runs through a node that is not one of its ends. */
  edgesCrossingNodes: string[];
  /** Pairs of edge ids that cross without sharing an endpoint. */
  edgeCrossings: string[];
  /** Node ids lying wholly outside the frame. */
  outsideFrame: string[];
  /** Node ids that are links, with the href each points at. */
  links: Record<string, string>;
}

/**
 * Sample every edge and compare it against every node and every other edge.
 *
 * jsdom performs no layout, so nothing in the unit suite can see an edge drawn
 * through a label or a node pushed off the canvas. This is the only place
 * those hold.
 */
export async function measureGraph(page: Page): Promise<GraphGeometry> {
  return page.evaluate(() => {
    const frame = document.querySelector('[data-testid="graph-frame"]')!.getBoundingClientRect();
    const nodes = [...document.querySelectorAll(".react-flow__node")].map((node) => {
      const box = node.getBoundingClientRect();

      return { id: (node as HTMLElement).dataset.id!, box, element: node };
    });

    const trace = (path: SVGPathElement) => {
      const length = path.getTotalLength();
      const matrix = path.getScreenCTM()!;

      return Array.from({ length: 201 }, (_, step) => {
        const point = path.getPointAtLength((length * step) / 200);

        return new DOMPoint(point.x, point.y).matrixTransform(matrix);
      });
    };

    const edges = [...document.querySelectorAll(".react-flow__edge")].map((edge) => ({
      id: (edge as HTMLElement).dataset.id!,
      points: trace(edge.querySelector(".react-flow__edge-path")!),
    }));

    const edgesCrossingNodes = edges
      .filter(({ id, points }) => {
        const ends = id.split("->");

        return points.some((point) =>
          nodes.some(
            ({ id: nodeId, box }) =>
              !ends.includes(nodeId) &&
              point.x > box.left &&
              point.x < box.right &&
              point.y > box.top &&
              point.y < box.bottom,
          ),
        );
      })
      .map(({ id }) => id);

    const intersects = (a: DOMPoint, b: DOMPoint, c: DOMPoint, d: DOMPoint) => {
      const denominator = (b.x - a.x) * (d.y - c.y) - (b.y - a.y) * (d.x - c.x);

      if (Math.abs(denominator) < 1e-9) {
        return false;
      }

      const t = ((c.x - a.x) * (d.y - c.y) - (c.y - a.y) * (d.x - c.x)) / denominator;
      const u = ((c.x - a.x) * (b.y - a.y) - (c.y - a.y) * (b.x - a.x)) / denominator;

      return t > 0.002 && t < 0.998 && u > 0.002 && u < 0.998;
    };

    const edgeCrossings: string[] = [];

    for (let first = 0; first < edges.length; first++) {
      for (let second = first + 1; second < edges.length; second++) {
        const ends = edges[first]!.id.split("->");

        if (edges[second]!.id.split("->").some((end) => ends.includes(end))) {
          continue;
        }

        const hit = edges[first]!.points.slice(0, -1).some((point, index) =>
          edges[second]!.points.slice(0, -1).some((other, otherIndex) =>
            intersects(
              point,
              edges[first]!.points[index + 1]!,
              other,
              edges[second]!.points[otherIndex + 1]!,
            ),
          ),
        );

        if (hit) {
          edgeCrossings.push(`${edges[first]!.id} × ${edges[second]!.id}`);
        }
      }
    }

    const links: Record<string, string> = {};

    for (const { id, element } of nodes) {
      const href = element.querySelector("a")?.getAttribute("href");

      if (href) {
        links[id] = href;
      }
    }

    return {
      nodes: nodes.length,
      edges: edges.length,
      edgesCrossingNodes,
      edgeCrossings,
      outsideFrame: nodes
        .filter(
          ({ box }) =>
            box.right <= frame.left ||
            box.left >= frame.right ||
            box.bottom <= frame.top ||
            box.top >= frame.bottom,
        )
        .map(({ id }) => id),
      links,
    };
  });
}
