# Personel Mobile Admin

Narrowly-scoped on-call admin app for Personel Phase 2.4. Built with Expo SDK 52 managed workflow.

**Scope**: 5 screens only — Sign In, Home (summary), Live View Approvals, DSR Queue, Silence Alerts. Full admin console remains at the web app.

---

## Prerequisites

- Node 20+
- pnpm 9+
- Expo CLI: `npm i -g expo-cli`
- EAS CLI: `npm i -g eas-cli`
- iOS: Xcode 15+ (for simulator or device testing)
- Android: Android Studio (for emulator) or a physical device

---

## Development workflow

### 1. Install dependencies

```bash
cd apps/mobile-admin
pnpm install
```

### 2. Configure environment

```bash
cp .env.example .env
# Edit .env — set MOBILE_BFF_URL, KEYCLOAK_URL, KEYCLOAK_REALM, KEYCLOAK_CLIENT_ID
```

### 3. Create a development build

The app uses `react-native-mmkv` (native module) so you cannot use Expo Go. You need a dev build.

```bash
# Build locally (requires Xcode / Android Studio)
npx expo run:ios
npx expo run:android

# OR build on EAS cloud (recommended for team development)
eas build --profile development --platform ios
eas build --profile development --platform android
```

Install the resulting `.ipa` / `.apk` on your device, then:

```bash
pnpm start   # starts Metro bundler
```

### 4. Push notification setup

Push notifications require a physical device. Configuration:

- **iOS (APNs)**: Configure via EAS secrets. Upload your APNs .p8 key in the EAS dashboard under "Credentials".
- **Android (FCM)**: Place your `google-services.json` in the project root. Set `GOOGLE_SERVICES_JSON` in `.env`.
- Push token registration is automatic on first authenticated launch. The token is POSTed to `mobile-bff /v1/mobile/push-tokens`.

---

## EAS Build profiles

Three profiles are configured in `eas.json`:

| Profile | Bundle ID | Distribution | Channel |
|---|---|---|---|
| `development` | `com.personel.admin.dev` | Internal | `development` |
| `preview` | `com.personel.admin.preview` | Internal (TestFlight / internal track) | `preview` |
| `production` | `com.personel.admin` | App Store / Play Store | `production` |

### Build commands

```bash
# Development
eas build --profile development --platform all

# Preview (TestFlight / Play Store internal)
eas build --profile preview --platform all

# Production
eas build --profile production --platform all
```

### Store submission

```bash
# iOS → App Store Connect (requires APPLE_ID, ASC_APP_ID, APPLE_TEAM_ID in eas.json)
eas submit --profile production --platform ios

# Android → Play Store internal track (requires service account JSON)
eas submit --profile production --platform android
```

---

## OTA updates (EAS Update)

```bash
# Publish to the production channel
eas update --channel production --message "fix: live view approval crash"

# Publish to preview channel
eas update --channel preview --message "feat: silence acknowledge modal"
```

Runtime version policy is `appVersion`. OTA updates are delivered when `EXPO_PUBLIC_APP_VARIANT` matches the build's channel. Breaking native changes require a new build; JS-only fixes can ship via OTA.

---

## Architecture overview

```
app/
├── _layout.tsx          ← root: QueryClient + MMKV init + push setup
├── index.tsx            ← redirect: /home or /sign-in
├── sign-in/index.tsx    ← OIDC PKCE (expo-auth-session + Keycloak)
├── (tabs)/              ← authenticated bottom-tab navigator
│   ├── home.tsx         ← summary cards from /v1/mobile/summary
│   ├── live-view.tsx    ← pending approval list
│   ├── dsr.tsx          ← open/at_risk/overdue DSR list
│   └── silence.tsx      ← Flow 7 gap list + acknowledge modal
├── live-view/[id].tsx   ← detail + Approve/Reject (dual-control guard)
└── dsr/[id].tsx         ← detail + Respond action

src/lib/
├── api/client.ts        ← fetch wrapper, RFC 7807, auto-refresh on 401
├── api/types.ts         ← OpenAPI-derived types
├── api/live-view.ts     ← TanStack Query keys + mutations
├── api/dsr.ts
├── api/silence.ts
├── api/home.ts
├── auth/oidc.ts         ← PKCE flow, token refresh, sign-out
├── auth/session.ts      ← zustand store + MMKV encrypted persistence
├── notifications/
│   ├── register.ts      ← Expo push token → mobile-bff registration
│   └── handlers.ts      ← foreground/background notification handling
└── i18n/tr.ts           ← Turkish string dictionary
```

### Push notification privacy (KVKK)

All push payloads sent from `mobile-bff` are sanitized to:
```json
{
  "type": "live_view_request | dsr_new | silence_alert | audit_spike",
  "count": 3,
  "deep_link": "personel://live-view/abc123"
}
```
No employee names, no endpoint identifiers, no DSR content. The app makes an authenticated API call to fetch details after the user taps. This is the ADR 0019 Push Privacy contract.

---

## Backend-developer dependencies

The following mobile-bff endpoints are required and not yet implemented:

| Endpoint | Purpose |
|---|---|
| `GET /v1/mobile/summary` | Aggregate: pending live-view count, pending DSR count, 24h silence count, last 5 audit entries |
| `POST /v1/mobile/push-tokens` | Register device push token (body: `{ token, platform, device_id }`) |
| Proxy `GET /v1/live-view/requests?state=REQUESTED` | Pending approval list |
| Proxy `GET /v1/live-view/requests/{id}` | Request detail |
| Proxy `POST /v1/live-view/requests/{id}/approve` | Approve (enforces approver ≠ requester) |
| Proxy `POST /v1/live-view/requests/{id}/reject` | Reject |
| Proxy `GET /v1/dsr?state=open,at_risk,overdue` | DSR queue |
| Proxy `GET /v1/dsr/{id}` | DSR detail |
| Proxy `POST /v1/dsr/{id}/respond` | Respond to DSR |
| Proxy `GET /v1/silence` | Silence gap list (with date_from/date_to filter) |
| Proxy `POST /v1/silence/{endpointId}/acknowledge` | Acknowledge silence gap |

All mobile-bff endpoints must validate the Bearer JWT from Keycloak before proxying to the Admin API. The `mobile-bff` Keycloak client is distinct from the console client.

---

## Development vs production scheme

| Setting | Dev | Preview | Production |
|---|---|---|---|
| Bundle ID | `com.personel.admin.dev` | `com.personel.admin.preview` | `com.personel.admin` |
| App Name | `Personel Admin (Dev)` | `Personel Admin (Preview)` | `Personel Admin` |
| EAS Update channel | `development` | `preview` | `production` |
| Distribution | Internal | Internal | App Store / Play Store |

Set `APP_VARIANT=development|preview|production` in your build environment (configured automatically in `eas.json`).
