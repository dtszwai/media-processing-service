import { expect, test } from "@playwright/test";

test("renders the local console shell", async ({ page }) => {
  await page.goto("/#/submit");

  await expect(page).toHaveTitle("submit · media-service");
  await expect(page.getByText(/media\s*·\s*service/)).toBeVisible();
  await expect(page.getByRole("navigation").getByRole("button", { name: "submit" })).toBeVisible();
});
