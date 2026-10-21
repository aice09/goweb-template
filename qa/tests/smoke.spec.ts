import { test, expect } from "@playwright/test";

test("login page renders and setup status endpoint responds", async ({ page, request }) => {
  const status = await request.get("/api/setup/status");
  expect(status.ok()).toBeTruthy();
  const payload = await status.json();
  expect(typeof payload.setup_required).toBe("boolean");

  await page.goto("/login");
  await expect(page.getByRole("heading", { name: /fast crud/i })).toBeVisible();
});
