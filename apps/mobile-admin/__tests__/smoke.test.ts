/**
 * Smoke test — verifies the auth session store compiles and initialises
 * without runtime errors. Does not test network calls or push notifications.
 *
 * Run with: pnpm test (or jest --watchAll=false from apps/mobile-admin)
 */

// Mock MMKV before the module is imported
jest.mock("react-native-mmkv", () => {
  return {
    MMKV: jest.fn().mockImplementation(() => ({
      set: jest.fn(),
      getString: jest.fn().mockReturnValue(null),
      delete: jest.fn(),
    })),
  };
});

jest.mock("expo-secure-store", () => ({
  getItemAsync: jest.fn().mockResolvedValue("test-encryption-key-hex"),
  setItemAsync: jest.fn().mockResolvedValue(undefined),
  WHEN_UNLOCKED_THIS_DEVICE_ONLY: "WHEN_UNLOCKED_THIS_DEVICE_ONLY",
}));

jest.mock("expo-constants", () => ({
  default: {
    expoConfig: {
      extra: {
        mobileBffUrl: "http://localhost:8090",
        keycloakUrl: "http://localhost:8180",
        keycloakRealm: "personel",
        keycloakClientId: "personel-mobile-admin",
      },
    },
  },
}));

describe("Auth session store", () => {
  it("initialises with unauthenticated state", async () => {
    const { useSessionStore } = await import("../src/lib/auth/session");
    const state = useSessionStore.getState();

    expect(state.isAuthenticated).toBe(false);
    expect(state.accessToken).toBeNull();
    expect(state.refreshToken).toBeNull();
    expect(state.user).toBeNull();
  });

  it("sets and clears a session", async () => {
    const { useSessionStore } = await import("../src/lib/auth/session");
    const store = useSessionStore.getState();

    store.setSession("access-token-test", "refresh-token-test", {
      sub: "user-123",
      email: "admin@example.com",
      username: "admin",
      roles: ["hr", "admin"],
      tenant_id: "tenant-abc",
    });

    const afterSet = useSessionStore.getState();
    expect(afterSet.isAuthenticated).toBe(true);
    expect(afterSet.accessToken).toBe("access-token-test");
    expect(afterSet.user?.sub).toBe("user-123");

    afterSet.clearSession();

    const afterClear = useSessionStore.getState();
    expect(afterClear.isAuthenticated).toBe(false);
    expect(afterClear.accessToken).toBeNull();
  });
});

describe("i18n tr dictionary", () => {
  it("exports required Turkish strings", async () => {
    const { tr } = await import("../src/lib/i18n/tr");

    expect(tr.liveView.selfApprovalBlocked).toBe(
      "Kendi talebinizi onaylayamazsınız.",
    );
    expect(tr.auth.loginButton).toBeTruthy();
    expect(tr.nav.liveView).toBeTruthy();
    expect(tr.nav.dsr).toBeTruthy();
    expect(tr.nav.silence).toBeTruthy();
  });
});

describe("API types", () => {
  it("ApiError carries status and problem detail", async () => {
    const { ApiError } = await import("../src/lib/api/types");

    const err = new ApiError(403, {
      title: "Yasaklı",
      status: 403,
      detail: "Bu kaynağa erişim reddedildi.",
    });

    expect(err.status).toBe(403);
    expect(err.message).toBe("Bu kaynağa erişim reddedildi.");
    expect(err.name).toBe("ApiError");
  });
});
