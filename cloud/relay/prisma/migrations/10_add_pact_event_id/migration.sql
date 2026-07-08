-- Track B (relay-ledger-idempotency): stable idempotency key (projectId, eventId).
-- Additive & nullable → safe via `prisma migrate deploy` on the shared Neon; linx
-- rows and existing pactify rows get eventId = NULL and keep deduping on
-- (projectId, seq). New pactify events carry a stable event_id and dedup on
-- (projectId, eventId). Postgres treats NULLs as distinct in a unique index, so
-- the many pre-existing NULL rows never collide.

-- AlterTable
ALTER TABLE "PactEvent" ADD COLUMN "eventId" TEXT;

-- CreateIndex
CREATE UNIQUE INDEX "PactEvent_projectId_eventId_key" ON "PactEvent"("projectId", "eventId");
