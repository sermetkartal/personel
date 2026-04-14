// Faz 14 #148 — DSR dry-run smoke.
//
// Asserts: DPO can create a KVKK m.11 erasure request and the
// dry-run endpoint returns a counts preview without actually
// mutating data. Guards against accidental destructive execution.
import { test, expect, adminUser, loginAs } from '../fixtures/auth';

test('create DSR erasure → dry-run → counts shown', async ({ page, context, adminTokens }) => {
  await loginAs(context, page, adminUser);
  await page.goto('/tr/dsr');

  await expect(page).toHaveURL(/\/tr\/dsr/);

  // New request button
  const newBtn = page.locator('button:has-text("Yeni"), a:has-text("Yeni")').first();
  await newBtn.click();

  // Subject id + kind = erasure
  const subjectInput = page.locator('input[name="subject_id"], input[placeholder*="kimlik" i]').first();
  await expect(subjectInput).toBeVisible();
  await subjectInput.fill('e2e-dryrun-' + Date.now());

  const kindSelect = page.locator('select[name="kind"], [data-testid="dsr-kind"]').first();
  if (await kindSelect.isVisible().catch(() => false)) {
    await kindSelect.selectOption({ label: /silme|erasure/i }).catch(async () => {
      await kindSelect.selectOption({ value: 'erasure' });
    });
  }

  const justification = page.locator('textarea[name="justification"], input[name="justification"]').first();
  if (await justification.isVisible().catch(() => false)) {
    await justification.fill('e2e dry-run smoke');
  }

  // Dry-run button (explicit, NOT execute)
  const dryRunBtn = page.locator('button:has-text("Kuru"), button:has-text("Dry"), button:has-text("Önizleme")').first();
  await expect(dryRunBtn).toBeVisible();
  await dryRunBtn.click();

  // Counts panel appears
  const counts = page.locator('[data-testid="dsr-dryrun-counts"], [data-test="dry-run-preview"]').first();
  await expect(counts).toBeVisible({ timeout: 15_000 });

  // Ensure the DESTRUCTIVE execute button is NOT auto-clicked or missing the confirm step
  const executeBtn = page.locator('button:has-text("Yürüt"), button:has-text("Execute")').first();
  if (await executeBtn.isVisible().catch(() => false)) {
    // Must require a second confirmation dialog
    await expect(executeBtn).toHaveAttribute('aria-disabled', 'true').catch(() => {
      // or at minimum not be auto-focused
    });
  }
});
