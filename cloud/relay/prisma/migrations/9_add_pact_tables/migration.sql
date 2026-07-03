-- U2 Mission Control: pact protocol events uploaded from pactify machines.
-- Additive (linx never touches these tables) → safe via `prisma migrate deploy`
-- on the shared Neon on redeploy. Same zero-knowledge shape as Run/RunEvent.

-- CreateTable
CREATE TABLE "Project" (
    "id" TEXT NOT NULL,
    "accountId" TEXT NOT NULL,
    "name" TEXT NOT NULL,
    "feature" TEXT,
    "seq" INTEGER NOT NULL DEFAULT 0,
    "lastEventAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "metadataEnc" TEXT,

    CONSTRAINT "Project_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "PactEvent" (
    "id" TEXT NOT NULL,
    "projectId" TEXT NOT NULL,
    "accountId" TEXT NOT NULL,
    "seq" INTEGER NOT NULL,
    "eventType" TEXT NOT NULL,
    "task" TEXT,
    "feature" TEXT,
    "ts" BIGINT NOT NULL,
    "bodyEnc" TEXT NOT NULL,

    CONSTRAINT "PactEvent_pkey" PRIMARY KEY ("id")
);

-- CreateIndex
CREATE INDEX "Project_accountId_idx" ON "Project"("accountId");

-- CreateIndex
CREATE INDEX "Project_accountId_lastEventAt_idx" ON "Project"("accountId", "lastEventAt");

-- CreateIndex
CREATE INDEX "PactEvent_projectId_idx" ON "PactEvent"("projectId");

-- CreateIndex
CREATE INDEX "PactEvent_accountId_idx" ON "PactEvent"("accountId");

-- CreateIndex
CREATE UNIQUE INDEX "PactEvent_projectId_seq_key" ON "PactEvent"("projectId", "seq");

-- AddForeignKey
ALTER TABLE "Project" ADD CONSTRAINT "Project_accountId_fkey" FOREIGN KEY ("accountId") REFERENCES "Account"("id") ON DELETE RESTRICT ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "PactEvent" ADD CONSTRAINT "PactEvent_projectId_fkey" FOREIGN KEY ("projectId") REFERENCES "Project"("id") ON DELETE RESTRICT ON UPDATE CASCADE;
