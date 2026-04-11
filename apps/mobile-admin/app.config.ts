import type { ExpoConfig, ConfigContext } from "expo/config";

const IS_DEV = process.env.APP_VARIANT === "development";
const IS_PREVIEW = process.env.APP_VARIANT === "preview";

const getBundleIdentifier = (): string => {
  if (IS_DEV) return "com.personel.admin.dev";
  if (IS_PREVIEW) return "com.personel.admin.preview";
  return "com.personel.admin";
};

const getAppName = (): string => {
  if (IS_DEV) return "Personel Admin (Dev)";
  if (IS_PREVIEW) return "Personel Admin (Preview)";
  return "Personel Admin";
};

export default ({ config }: ConfigContext): ExpoConfig => ({
  ...config,
  name: getAppName(),
  slug: "personel-mobile-admin",
  version: "1.0.0",
  orientation: "portrait",
  icon: "./assets/icon.png",
  scheme: "personel",
  userInterfaceStyle: "automatic",
  splash: {
    image: "./assets/splash.png",
    resizeMode: "contain",
    backgroundColor: "#0f172a",
  },
  ios: {
    supportsTablet: false,
    bundleIdentifier: getBundleIdentifier(),
    buildNumber: "1",
    infoPlist: {
      NSFaceIDUsageDescription:
        "Personel Admin, hassas işlemler için biyometrik kimlik doğrulama kullanır.",
      NSCameraUsageDescription:
        "Bu uygulama kamera kullanmaz.",
      UIBackgroundModes: ["remote-notification"],
    },
  },
  android: {
    adaptiveIcon: {
      foregroundImage: "./assets/adaptive-icon.png",
      backgroundColor: "#0f172a",
    },
    package: getBundleIdentifier(),
    versionCode: 1,
    permissions: [
      "android.permission.RECEIVE_BOOT_COMPLETED",
      "android.permission.VIBRATE",
      "android.permission.USE_BIOMETRIC",
      "android.permission.USE_FINGERPRINT",
    ],
    googleServicesFile: process.env.GOOGLE_SERVICES_JSON ?? "./google-services.json",
  },
  web: {
    bundler: "metro",
  },
  plugins: [
    "expo-router",
    "expo-secure-store",
    [
      "expo-notifications",
      {
        icon: "./assets/icon.png",
        color: "#0f172a",
        defaultChannel: "default",
      },
    ],
    [
      "expo-local-authentication",
      {
        faceIDPermission:
          "Personel Admin, hassas işlemler için Face ID kullanır.",
      },
    ],
    [
      "react-native-mmkv",
      {
        // Encrypted storage via expo-secure-store key
      },
    ],
  ],
  experiments: {
    typedRoutes: true,
  },
  updates: {
    url: "https://u.expo.dev/" + (process.env.EAS_PROJECT_ID ?? "REPLACE_WITH_EAS_PROJECT_ID"),
    enabled: true,
    fallbackToCacheTimeout: 0,
    checkAutomatically: "ON_LOAD",
    runtimeVersion: {
      policy: "appVersion",
    },
  },
  extra: {
    eas: {
      projectId: process.env.EAS_PROJECT_ID ?? "REPLACE_WITH_EAS_PROJECT_ID",
    },
    mobileBffUrl: process.env.MOBILE_BFF_URL ?? "http://localhost:8090",
    keycloakUrl: process.env.KEYCLOAK_URL ?? "http://localhost:8180",
    keycloakRealm: process.env.KEYCLOAK_REALM ?? "personel",
    keycloakClientId: process.env.KEYCLOAK_CLIENT_ID ?? "personel-mobile-admin",
  },
});
