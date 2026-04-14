// Faz 14 #148 — Audit log search smoke.
//
// Asserts: the audit viewer accepts a search term, the results area
// renders either rows or an explicit empty-state, and the
// hash-chain verify button is present (Faz 11 #117).
import { test, expect, adminUser, loginAs } from '../fixtures/auth';

test('search "endpoint.wipe" → results or empty state', async ({ page, context }) => {
  await loginAs(context, page, adminUser);
  await page.goto('/tr/audit');

  await expect(page).toHaveURL(/\/tr\/audit/);
  await expect(page.getByRole('heading', { level: 1 })).toBeVisible();

  // Search box
  const searchInput = page.locator('input[type="search"], input[placeholder*="ara" i]').first();
  await expect(searchInput).toBeVisible();
  await searchInput.fill('endpoint.wipe');
  await searchInput.press('Enter');

  // Wait for the results container — either rows or empty-state
  const results = page.locator('[data-testid="audit-results"], table tbody');
  await expect(results.first()).toBeVisible({ timeout: 15_000 });

  const rows = results.first().locator('tr, [data-testid^="audit-row-"]');
  const emptyState = page.locator('[data-testid="audit-empty-state"], text=Kayıt bulunamadı');

  const hasRows = await rows.count().catch(() => 0);
  const hasEmpty = await emptyState.isVisible().catch(() => false);

  expect(hasRows > 0 || hasEmpty).toBeTruthy();
});

test('audit viewer exposes chain verify action', async ({ page, context }) => {
  await loginAs(context, page, adminUser);
  await page.goto('/tr/audit');

  // Faz 11 #117 verify button / link
  const verifyBtn = page.locator(
    '[data-testid="audit-verify-chain"], button:has-text("Zincir"), button:has-text("Doğrula")',
  ).first();
  await expect(verifyBtn).toBeVisible();
});
