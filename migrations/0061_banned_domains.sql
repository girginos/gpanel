-- Banned domains (phishing prevention).
--
-- Replaces the misnamed cp_banned_tlds (was TLD blacklist; user actually
-- wanted full-domain blacklist). The purpose is to prevent tenants from
-- registering brand look-alike hostnames (sahibinden.com,
-- login.sahibinden.com, akbank.com, etc.) that would be used for phishing.
--
-- match_subdomains: when true (default), a ban on "sahibinden.com" also
-- rejects "x.sahibinden.com", "login.sahibinden.com" etc. Phishers hide the
-- brand in a subdomain, so this is the right default. Set to false only when
-- you want to block just the apex hostname (rare).
--
-- 🔴 DOES NOT AFFECT EXISTING DOMAINS. Only blocks NEW creations. Removing
-- live sites is a separate, explicit admin action.

DROP TABLE IF EXISTS cp_banned_tlds;

CREATE TABLE IF NOT EXISTS cp_banned_domains (
  domain            VARCHAR(253) NOT NULL PRIMARY KEY,
  description       VARCHAR(255) NOT NULL DEFAULT '',
  match_subdomains  TINYINT(1)   NOT NULL DEFAULT 1,
  created_by        BIGINT       NULL,
  created_at        DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
