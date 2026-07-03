-- AlterTable: surface the run's resolved, absolute working directory as
-- cleartext operational metadata, carried through the wire header alongside
-- "branch" (distinct from the E2E "workdirEnc" body field).
-- Nullable + additive: safe to apply via `prisma migrate deploy` on redeploy.
ALTER TABLE "Run" ADD COLUMN "workdir" TEXT;
