import { test, expect } from "./fixtures";

let serviceId: string;

test.beforeAll(async ({ request, baseURL }) => {
  const response = await request.get(`${baseURL}/services`, {
    headers: { Accept: "application/json" },
  });
  const data = await response.json();
  serviceId = data.items[0]?.ID;
});

const subResources = [
  "env",
  "labels",
  "resources",
  "placement",
  "ports",
  "update-policy",
  "rollback-policy",
  "log-driver",
  "configs",
  "secrets",
  "networks",
  "mounts",
  "container-config",
];

for (const sub of subResources) {
  test(`/services/:id/${sub} renders`, async ({ page }) => {
    test.skip(!serviceId, "No services available");
    await page.goto(`/services/${serviceId}/${sub}`);
    await expect(page.getByRole("link", { name: "Services" })).toBeVisible();
  });
}

test("/services/:id/<invalid> redirects to service detail", async ({ page }) => {
  test.skip(!serviceId, "No services available");
  await page.goto(`/services/${serviceId}/not-a-real-sub-resource`);
  await expect(page).toHaveURL(new RegExp(`/services/${serviceId}$`));
});
