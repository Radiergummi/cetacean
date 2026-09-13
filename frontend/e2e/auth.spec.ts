import { test, expect, apiJson, authProvider } from "./fixtures";

/**
 * ProfilePage redirects on the client, after /auth/whoami resolves. `goto`
 * returns well before that, so the URL at that moment says nothing about the
 * mode — deciding from it made each spec skip in the mode it was written for.
 */
test.describe("Auth / Profile", () => {
  test("profile page redirects to home when auth is none", async ({ page, request, baseURL }) => {
    const provider = await authProvider(request, baseURL);
    test.skip(provider !== "none", `Auth is ${provider} — the profile page renders instead`);

    await page.goto("/profile");

    await expect(page.getByRole("heading", { name: "Cluster Overview" })).toBeVisible({
      timeout: 10_000,
    });
    await expect(page).not.toHaveURL(/\/profile/);
  });

  test("profile page shows identity when auth is enabled", async ({ page, request, baseURL }) => {
    const provider = await authProvider(request, baseURL);
    test.skip(provider === "none", "Auth is none — /profile redirects to home");

    const identity = await apiJson(request, baseURL, "/auth/whoami");
    const name = String(identity.displayName || identity.subject);

    await page.goto("/profile");

    await expect(page).toHaveURL(/\/profile/);
    await expect(page.getByRole("heading", { name })).toBeVisible({ timeout: 10_000 });
  });
});
