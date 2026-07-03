-- Scale indexes for multi-account hot query patterns. Additive, no data change:
-- every statement is `CREATE INDEX IF NOT EXISTS`, so this is safe to apply via
-- `prisma migrate deploy` on a populated relay (and a no-op where the index
-- already exists from 0_init).
--
-- Hot paths these serve:
--   * listRuns            — WHERE accountId ORDER BY lastActiveAt DESC (board)
--   * listPendingApprovals/state filters — WHERE accountId AND state (inbox)
--   * listMachines        — WHERE accountId (Machine_accountId_idx, from 0_init)
--   * RunEvent catch-up   — WHERE runId (RunEvent_runId_idx FK index, from 0_init)

-- Board listing per account, recency-ordered: one composite index serves both
-- the accountId filter and the lastActiveAt sort.
CREATE INDEX IF NOT EXISTS "Run_accountId_lastActiveAt_idx" ON "Run"("accountId", "lastActiveAt");

-- Inbox / state-scoped board queries per account.
CREATE INDEX IF NOT EXISTS "Run_accountId_state_idx" ON "Run"("accountId", "state");

-- FK index on the per-account machine roster query. Already created by 0_init;
-- IF NOT EXISTS keeps this migration idempotent across environments.
CREATE INDEX IF NOT EXISTS "Machine_accountId_idx" ON "Machine"("accountId");

-- FK index for RunEvent catch-up (WHERE runId). Already created by 0_init;
-- IF NOT EXISTS guarantees the FK index exists even on schemas that predate it.
CREATE INDEX IF NOT EXISTS "RunEvent_runId_idx" ON "RunEvent"("runId");
