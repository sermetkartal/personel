# =============================================================================
# Personel Admin Console — Next.js 14 Production Build
# =============================================================================

FROM node:20-alpine AS deps

WORKDIR /app
COPY ../../apps/console/package.json ../../apps/console/package-lock.json* ./
RUN npm ci --only=production

FROM node:20-alpine AS builder

ARG PERSONEL_VERSION=0.1.0
ARG NEXT_PUBLIC_API_URL
ARG NEXT_PUBLIC_TENANT_ID

WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY ../../apps/console/ .

ENV NEXT_TELEMETRY_DISABLED=1
ENV NEXT_PUBLIC_API_URL=${NEXT_PUBLIC_API_URL}
ENV NEXT_PUBLIC_TENANT_ID=${NEXT_PUBLIC_TENANT_ID}

RUN npm run build

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
