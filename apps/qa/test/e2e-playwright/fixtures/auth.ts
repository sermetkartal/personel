// Faz 14 #148 — Auth fixture: Keycloak password grant token injection.
//
// Bypasses the interactive OIDC redirect by hitting Keycloak's
// /protocol/openid-connect/token with grant_type=password. The
// returned access_token is injected into the Next.js session cookie
// so the test lands on an authenticated page without UI clicks.
//
// This is ONLY safe in test realms. In production the direct-grant
// flow is disabled on the personel client.
import { test as base, BrowserContext, Page, APIRequestContext } from '@playwright/test';

const keycloakURL = process.env.PERSONEL_KEYCLOAK_URL ?? 'http://192.168.5.44:8080';
const realm = process.env.PERSONEL_REALM ?? 'personel';
const clientID = process.env.PERSONEL_CLIENT_ID ?? 'console';

export interface TestUser {
  username: string;
  password: string;
  role: 'admin' | 'dpo' | 'hr' | 'manager' | 'investigator' | 'employee';
  tenantID: string;
}

export const adminUser: TestUser = {
  username: process.env.PERSONEL_ADMIN_USER ?? 'admin',
  password: process.env.PERSONEL_ADMIN_PASSWORD ?? 'admin123',
  role: 'admin',
  tenantID: process.env.PERSONEL_TENANT_ID ?? 'be459dac-1a79-4054-b6e1-fa934a927315',
};

export const employeeUser: TestUser = {
  username: process.env.PERSONEL_EMPLOYEE_USER ?? 'zeynep',
  password: process.env.PERSONEL_EMPLOYEE_PASSWORD ?? 'employee123',
  role: 'employee',
  tenantID: process.env.PERSONEL_TENANT_ID ?? 'be459dac-1a79-4054-b6e1-fa934a927315',
};

export interface TokenSet {
  access_token: string;
  refresh_token: string;
  expires_in: number;
  id_token?: string;
}

export async function passwordGrant(
  request: APIRequestContext,
  user: TestUser,
): Promise<TokenSet> {
  const tokenURL = `${keycloakURL}/realms/${realm}/protocol/openid-connect/token`;
  const res = await request.post(tokenURL, {
    form: {
      grant_type: 'password',
      client_id: clientID,
      username: user.username,
      password: user.password,
      scope: 'openid profile email',
    },
    failOnStatusCode: false,
  });
  if (!res.ok()) {
    throw new Error(`Keycloak password grant failed ${res.status()}: ${await res.text()}`);
  }
  return (await res.json()) as TokenSet;
}

export async function loginAs(
  context: BrowserContext,
  page: Page,
  user: TestUser,
): Promise<TokenSet> {
  const tokens = await passwordGrant(context.request, user);
  // Next.js next-auth session cookie — schema depends on the app's
  // session provider. For the bearer-token variant we forward on
  // the Authorization header via a request context hook.
  await context.addCookies([
    {
      name: 'personel-access-token',
      value: tokens.access_token,
      url: page.url() || 'http://localhost:3000',
      httpOnly: true,
      sameSite: 'Lax',
    },
  ]);
  await context.setExtraHTTPHeaders({
    Authorization: `Bearer ${tokens.access_token}`,
  });
  return tokens;
}

export const test = base.extend<{ adminTokens: TokenSet }>({
  adminTokens: async ({ request }, use) => {
    const tokens = await passwordGrant(request, adminUser);
    await use(tokens);
  },
});

export { expect } from '@playwright/test';
