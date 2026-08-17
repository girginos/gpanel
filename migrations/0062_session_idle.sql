-- Session idle timeout. If a user is inactive for more than N minutes, their
-- token is rejected on next request. Admin sets N in `cp_ayarlar`.
--
-- 🔴 last_activity_ts is BUMPED on every authenticated request (throttled to
-- once per 30s to avoid write amplification on chatty polling endpoints).
-- 🔴 session_idle_minutes = 0 disables idle timeout entirely.
--
-- 🔴 EXISTING SESSIONS: after deploy, all currently-logged-in users start at
-- last_activity_ts=0 (column default). On their next request the middleware
-- updates it, so no one is instantly kicked out. If your policy is "kick
-- everyone immediately on deploy", bump users.token_gecersiz_ts as a separate
-- ONE-OFF operation — not something baked into this migration.

ALTER TABLE users
  ADD COLUMN last_activity_ts BIGINT NOT NULL DEFAULT 0;

INSERT IGNORE INTO cp_ayarlar (anahtar, deger) VALUES ('session_idle_minutes', '30');
