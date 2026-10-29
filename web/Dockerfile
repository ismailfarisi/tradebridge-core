# Production image for the TradeBridge web app (Next.js standalone output).
# NEXT_PUBLIC_CORE_URL is inlined at build time:
#   docker build --build-arg NEXT_PUBLIC_CORE_URL=https://api.example.com -t tradebridge-web web/
FROM node:20-alpine AS deps
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci

FROM node:20-alpine AS build
WORKDIR /app
ARG NEXT_PUBLIC_CORE_URL
ENV NEXT_PUBLIC_CORE_URL=$NEXT_PUBLIC_CORE_URL NEXT_TELEMETRY_DISABLED=1
COPY --from=deps /app/node_modules ./node_modules
COPY . .
RUN npm run build

FROM node:20-alpine AS run
WORKDIR /app
ENV NODE_ENV=production NEXT_TELEMETRY_DISABLED=1 PORT=3010 HOSTNAME=0.0.0.0
RUN addgroup -S app && adduser -S app -G app
COPY --from=build --chown=app:app /app/.next/standalone ./
COPY --from=build --chown=app:app /app/.next/static ./.next/static
COPY --from=build --chown=app:app /app/public ./public
USER app
EXPOSE 3010
HEALTHCHECK --interval=30s --timeout=3s CMD wget -qO- http://127.0.0.1:3010/ >/dev/null || exit 1
CMD ["node", "server.js"]
