-- AlterTable: add machine registry / presence columns.
-- Previous schema (0_init) had lastSeenAt as TIMESTAMP(3); convert existing
-- timestamps to epoch ms, then change the column to BIGINT for MachineInfo.
ALTER TABLE "Machine" ADD COLUMN "host" TEXT;
ALTER TABLE "Machine" ADD COLUMN "agentKinds" JSONB NOT NULL DEFAULT '[]';
ALTER TABLE "Machine" ADD COLUMN "workdirs" JSONB;
ALTER TABLE "Machine" ADD COLUMN "online" BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE "Machine" ALTER COLUMN "lastSeenAt" DROP DEFAULT;
ALTER TABLE "Machine" ALTER COLUMN "lastSeenAt" SET DATA TYPE BIGINT USING (EXTRACT(EPOCH FROM "lastSeenAt") * 1000)::bigint;
ALTER TABLE "Machine" ALTER COLUMN "lastSeenAt" SET DEFAULT 0;
