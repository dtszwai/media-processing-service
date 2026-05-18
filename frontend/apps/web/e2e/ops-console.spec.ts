import { expect, test } from "@playwright/test";

test("renders the LOCAL_ONLY console shell", async ({ page }) => {
  await page.goto("/#/submit");

  await expect(page).toHaveTitle("submit · media-service");
  await expect(page.getByText(/media\s*·\s*service/)).toBeVisible();
  await expect(page.getByText("ops console")).toBeVisible();
  await expect(page.getByText("LOCAL_ONLY")).toBeVisible();
  await expect(page.getByRole("navigation").getByRole("button", { name: "submit" })).toBeVisible();
});
