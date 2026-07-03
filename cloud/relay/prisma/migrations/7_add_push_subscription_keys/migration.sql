-- Web-push delivery (i-notif-be): persist the PushSubscription `keys`
-- (p256dh + auth) alongside the existing PushToken row so the relay can encrypt
-- a web-push. Additive + nullable so the migration is back-compat with any
-- pre-existing rows. The PushSubscription `endpoint` is stored in `token`, so
-- the existing UNIQUE(accountId, token) already enforces idempotency by endpoint.
ALTER TABLE "PushToken" ADD COLUMN "keysP256dh" TEXT;
ALTER TABLE "PushToken" ADD COLUMN "keysAuth" TEXT;
