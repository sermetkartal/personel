// Faz 14 #148 — Console login happy path smoke.
//
// Asserts: admin can log in via Keycloak password grant, lands on
// the Turkish dashboard, and at least one showcase employee tile is
// rendered in the grid.
import { test, expect, adminUser, loginAs } from '../fixtures/auth';
import { ensureShowcaseEmployees } from '../fixtures/seed';

test('admin login → dashboard → showcase employees visible', async ({ page, context, adminTokens }) => {
  await ensureShowcaseEmployees({ request: context.request, accessToken: adminTokens.access_token });
  await loginAs(context, page, adminUser);

  await page.goto('/tr/dashboard');
  await expect(page).toHaveURL(/\/tr\/dashboard/);

  // Nav chrome is visible
  await expect(page.locator('nav, [role="navigation"]')).toBeVisible();

  // Dashboard loads a heading in Turkish
  await expect(page.getByRole('heading', { level: 1 })).toBeVisible();

  // At least one employee card / row in the grid
  const grid = page.locator('[data-testid="employee-grid"], [data-test="employee-list"]').first();
  if (await grid.isVisible().catch(() => false)) {
    const rows = grid.locator('[data-testid^="employee-row-"], [data-test^="employee-card-"]');
    await expect(rows.first()).toBeVisible({ timeout: 10_000 });
  } else {
    // Fallback selector: any link to an employee detail page
    const link = page.locator('a[href*="/employees/"]').first();
    await expect(link).toBeVisible({ timeout: 10_000 });
  }
});

test('dashboard renders key chrome: sidebar + profile menu', async ({ page, context, adminTokens }) => {
  await loginAs(context, page, adminUser);
  await page.goto('/tr/dashboard');

  // Sidebar
  const sidebar = page.locator('aside, [data-testid="sidebar"]').first();
  await expect(sidebar).toBeVisible();

  // Profile / user menu in header
  const profileMenu = page.locator('[data-testid="user-menu"], button[aria-label*="profil" i]').first();
  await expect(profileMenu).toBeVisible();
});
