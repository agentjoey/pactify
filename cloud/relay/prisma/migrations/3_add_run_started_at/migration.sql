-- AlterTable: surface the run's start time (epoch ms) as cleartext operational
-- metadata, carried through the wire header so the web can tick a running timer.
-- Named "startedAtMs" because the legacy "startedAt" TIMESTAMP column (a
-- server-side default-now stamp) already occupies that name.
-- Nullable + additive: safe to apply via `prisma migrate deploy` on redeploy.
ALTER TABLE "Run" ADD COLUMN "startedAtMs" BIGINT;
