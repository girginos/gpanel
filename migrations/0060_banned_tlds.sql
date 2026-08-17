-- Banned top-level domains. Managed by admin; every domain creation path runs
-- through provisioner.ValidateDomain which rejects domains whose TLD is in
-- this list.
--
-- 🔴 DOES NOT AFFECT EXISTING DOMAINS. Only blocks NEW creations. Otherwise a
-- newly banned TLD would take live customer sites offline — irreversible SEO,
-- email and trust damage. Deletion of live domains must remain a separate,
-- explicit admin action.
--
-- Naming note: table and column names are English by convention going forward,
-- even though the wider schema mixes English (`cp_ayarlar`) and Turkish
-- (`cp_mail_iletim`). New tables English.

CREATE TABLE IF NOT EXISTS cp_banned_tlds (
  tld           VARCHAR(63)  NOT NULL PRIMARY KEY,
  description   VARCHAR(255) NOT NULL DEFAULT '',
  created_by    BIGINT       NULL,
  created_at    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
