import createNextIntlPlugin from "next-intl/plugin";

const withNextIntl = createNextIntlPlugin("./src/lib/i18n/config.ts");

/** @type {import('next').NextConfig} */
const nextConfig = {
  output: "standalone",

  experimental: {
    // Reality check 2026-04-11: typedRoutes disabled (rejects template-literal
    // URLs in router.replace). instrumentationHook deprecated (Next 15 has
    // instrumentation.js by default). Tech debt: revisit typed route helpers.
    typedRoutes: false,
  },

  images: {
    formats: ["image/avif", "image/webp"],
    remotePatterns: [
      {
        protocol: "https",
        hostname: process.env.MINIO_HOSTNAME ?? "minio.personel.local",
        pathname: "/**",
      },
    ],
    minimumCacheTTL: 60,
  },

  // Wave 9: KVKK menü yeniden yapılandırması sonrası eski URL'leri koru.
  // Bookmark kırılmasın, audit log referansları çalışmaya devam etsin.
  redirects: async () => [
    {
      source: "/:locale(tr|en)/dsr/:path*",
      destination: "/:locale/kvkk/dsr/:path*",
      permanent: true,
    },
    {
      source: "/:locale(tr|en)/legal-hold/:path*",
      destination: "/:locale/kvkk/legal-hold/:path*",
      permanent: true,
    },
    {
      source: "/:locale(tr|en)/destruction-reports/:path*",
      destination: "/:locale/kvkk/destruction-reports/:path*",
      permanent: true,
    },
    {
      source: "/:locale(tr|en)/settings/dlp",
      destination: "/:locale/kvkk/dlp",
      permanent: true,
    },
  ],

  headers: async () => [
    {
      source: "/(.*)",
      headers: [
        { key: "X-Frame-Options", value: "DENY" },
        { key: "X-Content-Type-Options", value: "nosniff" },
        { key: "Referrer-Policy", value: "strict-origin-when-cross-origin" },
        {
          key: "Permissions-Policy",
          value: "camera=(), microphone=(), geolocation=()",
        },
        {
          key: "Content-Security-Policy",
          value: [
            "default-src 'self'",
            "script-src 'self' 'unsafe-inline' 'unsafe-eval'",
            "style-src 'self' 'unsafe-inline'",
            (() => {
              const api = process.env.NEXT_PUBLIC_API_BASE_URL ?? "";
              const kc = process.env.NEXT_PUBLIC_KEYCLOAK_URL ?? "";
              const lk = process.env.NEXT_PUBLIC_LIVEKIT_URL ?? "";
              // WebSocket endpoints for audit stream + live view pubsub
              // derive from API base URL (http -> ws, https -> wss).
              const apiWs = api
                ? api.replace(/^http/, "ws")
                : "";
              return `connect-src 'self' ${api} ${kc} ${lk} ${apiWs}`;
            })(),
            "img-src 'self' data: blob: https:",
            "frame-ancestors 'none'",
          ]
            .filter(Boolean)
            .join("; "),
        },
      ],
    },
  ],

  logging: {
    fetches: {
      fullUrl: process.env.NODE_ENV === "development",
    },
  },

  eslint: {
    ignoreDuringBuilds: false,
  },

  typescript: {
    ignoreBuildErrors: false,
  },
};

export default withNextIntl(nextConfig);
