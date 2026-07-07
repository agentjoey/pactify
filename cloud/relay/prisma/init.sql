-- CreateSchema
CREATE SCHEMA IF NOT EXISTS "public";

-- CreateTable
CREATE TABLE "Account" (
    "id" TEXT NOT NULL,
    "publicKey" TEXT NOT NULL,
    "seq" INTEGER NOT NULL DEFAULT 0,
    "settingsEnc" TEXT,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "Account_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "Device" (
    "id" TEXT NOT NULL,
    "accountId" TEXT NOT NULL,
    "publicKey" TEXT NOT NULL,
    "pushToken" TEXT,
    "pairedAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "Device_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "Machine" (
    "id" TEXT NOT NULL,
    "accountId" TEXT NOT NULL,
    "metadataEnc" TEXT NOT NULL,
    "presence" TEXT NOT NULL DEFAULT 'offline',
    "cliAvailability" TEXT,
    "host" TEXT,
    "agentKinds" JSONB NOT NULL DEFAULT '[]',
    "workdirs" JSONB,
    "online" BOOLEAN NOT NULL DEFAULT false,
    "lastSeenAt" BIGINT NOT NULL DEFAULT 0,

    CONSTRAINT "Machine_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "Run" (
    "id" TEXT NOT NULL,
    "accountId" TEXT NOT NULL,
    "machineId" TEXT NOT NULL,
    "agentKind" TEXT NOT NULL,
    "state" TEXT NOT NULL,
    "pendingApprovals" INTEGER NOT NULL DEFAULT 0,
    "tokensIn" INTEGER NOT NULL DEFAULT 0,
    "tokensOut" INTEGER NOT NULL DEFAULT 0,
    "costMicros" INTEGER,
    "seq" INTEGER NOT NULL DEFAULT 0,
    "startedAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "lastActiveAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "endedAt" TIMESTAMP(3),
    "branch" TEXT,
    "startedAtMs" BIGINT,
    "workdir" TEXT,
    "repoRoot" TEXT,
    "title" TEXT,
    "titleEnc" TEXT,
    "workdirEnc" TEXT,
    "modelEnc" TEXT,
    "taskEnc" TEXT,
    "snapshotEnc" TEXT,
    "dataEncryptionKey" TEXT,

    CONSTRAINT "Run_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "RunEvent" (
    "id" TEXT NOT NULL,
    "runId" TEXT NOT NULL,
    "seq" INTEGER NOT NULL,
    "state" TEXT NOT NULL,
    "eventKind" TEXT NOT NULL,
    "ts" BIGINT NOT NULL,
    "bodyEnc" TEXT NOT NULL,

    CONSTRAINT "RunEvent_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "PushToken" (
    "id" TEXT NOT NULL,
    "accountId" TEXT NOT NULL,
    "token" TEXT NOT NULL,
    "platform" TEXT NOT NULL,
    "keysP256dh" TEXT,
    "keysAuth" TEXT,

    CONSTRAINT "PushToken_pkey" PRIMARY KEY ("id")
);

-- CreateIndex
CREATE UNIQUE INDEX "Account_publicKey_key" ON "Account"("publicKey");

-- CreateIndex
CREATE INDEX "Device_accountId_idx" ON "Device"("accountId");

-- CreateIndex
CREATE INDEX "Machine_accountId_idx" ON "Machine"("accountId");

-- CreateIndex
CREATE INDEX "Run_accountId_idx" ON "Run"("accountId");

-- CreateIndex
CREATE INDEX "Run_machineId_idx" ON "Run"("machineId");

-- CreateIndex
CREATE INDEX "Run_accountId_lastActiveAt_idx" ON "Run"("accountId", "lastActiveAt");

-- CreateIndex
CREATE INDEX "Run_accountId_state_idx" ON "Run"("accountId", "state");

-- CreateIndex
CREATE INDEX "RunEvent_runId_idx" ON "RunEvent"("runId");

-- CreateIndex
CREATE UNIQUE INDEX "RunEvent_runId_seq_key" ON "RunEvent"("runId", "seq");

-- CreateIndex
CREATE UNIQUE INDEX "PushToken_accountId_token_key" ON "PushToken"("accountId", "token");

-- AddForeignKey
ALTER TABLE "Device" ADD CONSTRAINT "Device_accountId_fkey" FOREIGN KEY ("accountId") REFERENCES "Account"("id") ON DELETE RESTRICT ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "Machine" ADD CONSTRAINT "Machine_accountId_fkey" FOREIGN KEY ("accountId") REFERENCES "Account"("id") ON DELETE RESTRICT ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "Run" ADD CONSTRAINT "Run_accountId_fkey" FOREIGN KEY ("accountId") REFERENCES "Account"("id") ON DELETE RESTRICT ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "Run" ADD CONSTRAINT "Run_machineId_fkey" FOREIGN KEY ("machineId") REFERENCES "Machine"("id") ON DELETE RESTRICT ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "RunEvent" ADD CONSTRAINT "RunEvent_runId_fkey" FOREIGN KEY ("runId") REFERENCES "Run"("id") ON DELETE RESTRICT ON UPDATE CASCADE;


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
    "eventId" TEXT,
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

-- CreateIndex
CREATE UNIQUE INDEX "PactEvent_projectId_eventId_key" ON "PactEvent"("projectId", "eventId");

-- AddForeignKey
ALTER TABLE "Project" ADD CONSTRAINT "Project_accountId_fkey" FOREIGN KEY ("accountId") REFERENCES "Account"("id") ON DELETE RESTRICT ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "PactEvent" ADD CONSTRAINT "PactEvent_projectId_fkey" FOREIGN KEY ("projectId") REFERENCES "Project"("id") ON DELETE RESTRICT ON UPDATE CASCADE;
