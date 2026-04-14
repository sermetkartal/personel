# =============================================================================
# Personel Admin Console — Next.js 15 Production Build
#
# Build context: repository root (docker-compose.yaml sets build.context: ../..).
# Package manager: pnpm (see apps/console/pnpm-lock.yaml). A previous version
# of this Dockerfile used npm ci which would have silently installed the wrong
# dependency tree. Fixed 2026-04-11 during the first real Docker build.
# =============================================================================

FROM node:20-alpine AS deps

# Enable Corepack so pnpm is available without a separate install step.
# Corepack ships with Node 20 and resolves the pinned pnpm version from
# package.json "packageManager" field if present.
RUN corepack enable

WORKDIR /app
COPY apps/console/package.json apps/console/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile --prod=false

FROM node:20-alpine AS builder

ARG PERSONEL_VERSION=0.1.0
ARG NEXT_PUBLIC_API_BASE_URL
ARG NEXT_PUBLIC_KEYCLOAK_URL
ARG NEXT_PUBLIC_KEYCLOAK_REALM=personel
ARG NEXT_PUBLIC_KEYCLOAK_CLIENT_ID=personel-console
ARG NEXT_PUBLIC_LIVEKIT_URL
ARG NEXT_PUBLIC_TENANT_ID

RUN corepack enable

WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY apps/console/ .

ENV NEXT_TELEMETRY_DISABLED=1
ENV NEXT_PUBLIC_API_BASE_URL=${NEXT_PUBLIC_API_BASE_URL}
ENV NEXT_PUBLIC_KEYCLOAK_URL=${NEXT_PUBLIC_KEYCLOAK_URL}
ENV NEXT_PUBLIC_KEYCLOAK_REALM=${NEXT_PUBLIC_KEYCLOAK_REALM}
ENV NEXT_PUBLIC_KEYCLOAK_CLIENT_ID=${NEXT_PUBLIC_KEYCLOAK_CLIENT_ID}
ENV NEXT_PUBLIC_LIVEKIT_URL=${NEXT_PUBLIC_LIVEKIT_URL}
ENV NEXT_PUBLIC_TENANT_ID=${NEXT_PUBLIC_TENANT_ID}

RUN pnpm build

FROM node:20-alpine AS runner

ARG PERSONEL_VERSION=0.1.0

WORKDIR /app
ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1

RUN addgroup --system --gid 1001 nodejs \
 && adduser --system --uid 1001 nextjs

COPY --from=builder /app/public ./public
COPY --from=builder --chown=nextjs:nodejs /app/.next/standalone ./
COPY --from=builder --chown=nextjs:nodejs /app/.next/static ./.next/static

USER nextjs
EXPOSE 3000
ENV PORT=3000
ENV HOSTNAME="0.0.0.0"

CMD ["node", "server.js"]

LABEL org.opencontainers.image.title="personel-console"
LABEL com.personel.service="console"
