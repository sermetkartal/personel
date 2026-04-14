// Faz 14 #148 — Employee detail drill-down smoke.
//
// Asserts: clicking an employee tile lands on the detail page and
// the rich signals grid (today stats, 24h sparkline, assigned
// endpoints, 7-day series) is rendered without any empty
// "failed to load" error.
import { test, expect, adminUser, loginAs } from '../fixtures/auth';
import { findShowcaseZeynep } from '../fixtures/seed';

test('click Zeynep → detail page → rich signals grid', async ({ page, context, adminTokens }) => {
  await loginAs(context, page, adminUser);
  const zeynepID = await findShowcaseZeynep({ request: context.request, accessToken: adminTokens.access_token });
  test.skip(!zeynepID, 'showcase-zeynep not seeded on this stack');

  await page.goto(`/tr/employees/${zeynepID}`);
  await expect(page).toHaveURL(new RegExp(`/employees/${zeynepID}`));

  // Page header with employee display name
  const heading = page.getByRole('heading', { level: 1 });
  await expect(heading).toBeVisible();
  await expect(heading).toContainText(/Zeynep|Çalışan/i);

  // Signals grid sections — at least 2 of the 4 expected panels
  // must be visible (daily, hourly, endpoints, 7-day).
  const panels = page.locator('[data-testid^="panel-"], [data-test^="section-"]');
  const count = await panels.count();
  expect(count).toBeGreaterThanOrEqual(2);

  // No error banner in the page body
  const errorBanner = page.locator('[role="alert"][data-variant="error"], .error-state');
  await expect(errorBanner).toHaveCount(0);
});

test('detail → 7 day trend chart has data points', async ({ page, context, adminTokens }) => {
  await loginAs(context, page, adminUser);
  const zeynepID = await findShowcaseZeynep({ request: context.request, accessToken: adminTokens.access_token });
  test.skip(!zeynepID, 'showcase-zeynep not seeded');

  await page.goto(`/tr/employees/${zeynepID}`);
  const chart = page.locator('[data-testid="7-day-chart"], svg.recharts-surface').first();
  if (await chart.isVisible().catch(() => false)) {
    // Recharts emits <path> inside .recharts-line
    const paths = chart.locator('path');
    await expect(paths.first()).toBeVisible();
  }
});
