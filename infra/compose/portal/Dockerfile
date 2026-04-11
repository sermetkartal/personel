# =============================================================================
# Personel Transparency Portal — Next.js 14 Production Build
# =============================================================================

FROM node:20-alpine AS deps

WORKDIR /app
COPY ../../apps/portal/package.json ../../apps/portal/package-lock.json* ./
RUN npm ci --only=production

FROM node:20-alpine AS builder

ARG PERSONEL_VERSION=0.1.0
ARG NEXT_PUBLIC_API_URL
ARG NEXT_PUBLIC_TENANT_ID

WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY ../../apps/portal/ .

ENV NEXT_TELEMETRY_DISABLED=1
ENV NEXT_PUBLIC_API_URL=${NEXT_PUBLIC_API_URL}
ENV NEXT_PUBLIC_TENANT_ID=${NEXT_PUBLIC_TENANT_ID}
ENV PORT=3001

RUN npm run build

FROM node:20-alpine AS runner

WORKDIR /app
ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1

RUN addgroup --system --gid 1001 nodejs \
 && adduser --system --uid 1001 nextjs

COPY --from=builder /app/public ./public
COPY --from=builder --chown=nextjs:nodejs /app/.next/standalone ./
COPY --from=builder --chown=nextjs:nodejs /app/.next/static ./.next/static

USER nextjs
EXPOSE 3001
ENV PORT=3001
ENV HOSTNAME="0.0.0.0"

CMD ["node", "server.js"]

LABEL org.opencontainers.image.title="personel-portal"
LABEL com.personel.service="portal"
