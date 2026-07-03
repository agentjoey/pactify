# Build context: cloud/ (the pnpm workspace root for this monorepo).
# Referenced as `dockerfile = "relay/Dockerfile"` from cloud/fly.toml (which
# lives at the cloud/ root so the path resolves against this build context).
#   fly deploy                    # from cloud/, production
#   fly deploy -c fly.staging.toml
#
# Runtime applies the schema to Neon (prisma migrate deploy) then starts the
# bootstrap (dist/server-main.js). DATABASE_URL + RELAY_SECRET are injected at
# run time (Fly secrets); PORT defaults to 4310.

# ── base: node + corepack-pinned pnpm ────────────────────────────────────────
FROM node:24-slim AS base
ENV PNPM_HOME=/pnpm
ENV PATH="$PNPM_HOME:$PATH"
RUN corepack enable
WORKDIR /app

# ── deps + build ─────────────────────────────────────────────────────────────
FROM base AS build
COPY pnpm-workspace.yaml pnpm-lock.yaml .npmrc package.json ./
COPY wire/package.json wire/package.json
COPY relay/package.json relay/package.json
RUN pnpm install --frozen-lockfile

COPY tsconfig.base.json ./
COPY wire wire
COPY relay relay
RUN pnpm --filter @pactify-apps/relay... build

# ── runtime ──────────────────────────────────────────────────────────────────
FROM base AS runtime
ENV NODE_ENV=production
WORKDIR /app/relay

COPY --from=build /app/node_modules /app/node_modules
COPY --from=build /app/package.json /app/package.json
COPY --from=build /app/pnpm-workspace.yaml /app/pnpm-workspace.yaml
COPY --from=build /app/wire /app/wire
COPY --from=build /app/relay/node_modules /app/relay/node_modules
COPY --from=build /app/relay/dist /app/relay/dist
COPY --from=build /app/relay/prisma /app/relay/prisma
COPY --from=build /app/relay/prisma.config.ts /app/relay/prisma.config.ts
COPY --from=build /app/relay/package.json /app/relay/package.json

EXPOSE 4310
CMD ["sh", "-c", "pnpm prisma migrate deploy && node dist/server-main.js"]
