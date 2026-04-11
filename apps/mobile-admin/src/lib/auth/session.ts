/**
 * Zustand auth session store with MMKV persistence.
 * The MMKV encryption key is bootstrapped from expo-secure-store
 * so token storage is encrypted at rest on device.
 *
 * KVKK: No employee PII is stored in the session.
 * Only admin user's own JWT claims (sub, email, roles) are retained.
 * Session is cleared on logout or 401-after-refresh.
 */

import { create } from "zustand";
import { persist, createJSONStorage } from "zustand/middleware";
import { MMKV } from "react-native-mmkv";
import * as SecureStore from "expo-secure-store";

// ── MMKV encrypted storage ────────────────────────────────────────────────────

const MMKV_ENCRYPTION_KEY_NAME = "personel_mmkv_encryption_key";

let _storage: MMKV | null = null;

async function getEncryptedStorage(): Promise<MMKV> {
  if (_storage) return _storage;

  let encryptionKey = await SecureStore.getItemAsync(MMKV_ENCRYPTION_KEY_NAME);
  if (!encryptionKey) {
    // Generate a 32-byte random key and persist it in SecureStore (device keychain)
    const bytes = new Uint8Array(32);
    crypto.getRandomValues(bytes);
    encryptionKey = Buffer.from(bytes).toString("hex");
    await SecureStore.setItemAsync(MMKV_ENCRYPTION_KEY_NAME, encryptionKey, {
      keychainAccessible: SecureStore.WHEN_UNLOCKED_THIS_DEVICE_ONLY,
    });
  }

  _storage = new MMKV({
    id: "personel-session",
    encryptionKey,
  });
  return _storage;
}

// Synchronous MMKV storage adapter for zustand/persist
// The storage is initialised lazily; first write triggers key bootstrap.
const mmkvZustandStorage = {
  getItem: (name: string): string | null => {
    if (!_storage) return null;
    return _storage.getString(name) ?? null;
  },
  setItem: (name: string, value: string): void => {
    if (_storage) {
      _storage.set(name, value);
    } else {
      // Bootstrap async then re-set — first persist write may be dropped
      // on very first launch before the key is ready; this is acceptable
      // because the session has not been established yet.
      void getEncryptedStorage().then((s) => s.set(name, value));
    }
  },
  removeItem: (name: string): void => {
    _storage?.delete(name);
  },
};

export async function initSessionStorage(): Promise<void> {
  await getEncryptedStorage();
}

// ── Session store ─────────────────────────────────────────────────────────────

export interface SessionUser {
  sub: string;
  email: string;
  username: string;
  roles: string[];
  tenant_id: string;
}

interface SessionState {
  accessToken: string | null;
  refreshToken: string | null;
  user: SessionUser | null;
  isAuthenticated: boolean;

  setSession: (
    accessToken: string,
    refreshToken: string,
    user: SessionUser,
  ) => void;
  clearSession: () => void;
  refreshTokens: () => Promise<boolean>;
}

export const useSessionStore = create<SessionState>()(
  persist(
    (set, get) => ({
      accessToken: null,
      refreshToken: null,
      user: null,
      isAuthenticated: false,

      setSession: (accessToken, refreshToken, user) => {
        set({ accessToken, refreshToken, user, isAuthenticated: true });
      },

      clearSession: () => {
        set({
          accessToken: null,
          refreshToken: null,
          user: null,
          isAuthenticated: false,
        });
      },

      refreshTokens: async (): Promise<boolean> => {
        const { refreshToken } = get();
        if (!refreshToken) return false;

        try {
          // Import here to avoid circular dependency with oidc.ts
          const { refreshAccessToken } = await import("@/lib/auth/oidc");
          const result = await refreshAccessToken(refreshToken);
          set({
            accessToken: result.accessToken,
            refreshToken: result.refreshToken,
            isAuthenticated: true,
          });
          return true;
        } catch {
          set({
            accessToken: null,
            refreshToken: null,
            user: null,
            isAuthenticated: false,
          });
          return false;
        }
      },
    }),
    {
      name: "personel-session-v1",
      storage: createJSONStorage(() => mmkvZustandStorage),
      // Only persist tokens and user; do NOT persist any employee data
      partialize: (state) => ({
        accessToken: state.accessToken,
        refreshToken: state.refreshToken,
        user: state.user,
        isAuthenticated: state.isAuthenticated,
      }),
    },
  ),
);
