-- AlterTable: surface the run's git branch as cleartext operational metadata.
-- Nullable + additive: safe to apply via `prisma migrate deploy` on redeploy.
ALTER TABLE "Run" ADD COLUMN "branch" TEXT;
