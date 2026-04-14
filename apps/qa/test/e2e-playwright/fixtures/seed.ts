// Faz 14 #148 — Seed data helper for E2E tests.
//
// Idempotent: every helper is safe to call multiple times. Uses the
// admin API directly (not the UI) to pre-populate data that the UI
// tests then assert against.
import { APIRequestContext } from '@playwright/test';

const apiURL = process.env.PERSONEL_API_URL ?? 'http://192.168.5.44:8000';

export interface SeedContext {
  request: APIRequestContext;
  accessToken: string;
}

export async function ensureShowcaseEmployees(ctx: SeedContext): Promise<void> {
  const res = await ctx.request.get(`${apiURL}/v1/employees?limit=5`, {
    headers: { Authorization: `Bearer ${ctx.accessToken}` },
    failOnStatusCode: false,
  });
  if (res.ok()) {
    const body = await res.json();
    if (Array.isArray(body.items) && body.items.length >= 1) {
      return; // already seeded
    }
  }
  // Showcase seed endpoint is idempotent on the backend; if it does
  // not exist yet, the test simply uses whatever data is available.
  await ctx.request.post(`${apiURL}/v1/admin/seed/showcase`, {
    headers: { Authorization: `Bearer ${ctx.accessToken}` },
    data: {},
    failOnStatusCode: false,
  });
}

export async function createTestDSR(
  ctx: SeedContext,
  subjectID: string,
  kind: 'access' | 'erasure' = 'access',
): Promise<string> {
  const res = await ctx.request.post(`${apiURL}/v1/dsr/requests`, {
    headers: { Authorization: `Bearer ${ctx.accessToken}` },
    data: {
      subject_id: subjectID,
      kind,
      legal_basis: 'KVKK m.11',
      justification: 'e2e test',
      test: true,
    },
    failOnStatusCode: false,
  });
  if (!res.ok()) {
    throw new Error(`seed DSR failed ${res.status()}: ${await res.text()}`);
  }
  const body = await res.json();
  return body.id as string;
}

export async function findShowcaseZeynep(ctx: SeedContext): Promise<string | null> {
  const res = await ctx.request.get(`${apiURL}/v1/employees?q=Zeynep&limit=5`, {
    headers: { Authorization: `Bearer ${ctx.accessToken}` },
    failOnStatusCode: false,
  });
  if (!res.ok()) return null;
  const body = await res.json();
  if (!Array.isArray(body.items)) return null;
  for (const item of body.items) {
    if (typeof item?.display_name === 'string' && item.display_name.includes('Zeynep')) {
      return item.id as string;
    }
  }
  return body.items[0]?.id ?? null;
}
