-- AlterTable: surface the run's git repo root as cleartext operational metadata,
-- carried through the wire header alongside "workdir"/"branch". Groups a run by
-- its PROJECT (repo) rather than a per-subdir workdir.
-- Nullable + additive: safe to apply via `prisma migrate deploy` on redeploy.
ALTER TABLE "Run" ADD COLUMN "repoRoot" TEXT;
