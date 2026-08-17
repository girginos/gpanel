-- Website Security Monitor — cross-ecosystem vulnerability findings.
--
-- Schema is INTENTIONALLY generic (app_type + package_name) so Node.js and
-- PHP Composer scanners can share the same table when they're added. First
-- shipped scanner: WordPress plugins/themes/core (feed: wpvulnerability.net).
--
-- 🔴 UNIQUE KEY covers (domain_id, install_path, package_name, cve_id) so the
-- same finding isn't double-counted when a scan runs again. `last_seen` bumps
-- on every rescan; a finding whose `last_seen` gets stale (package removed
-- or upgraded past the vulnerable range) is dropped from summary counts by
-- the aggregator, not the scanner.

CREATE TABLE IF NOT EXISTS cp_websec_findings (
  id                BIGINT       AUTO_INCREMENT PRIMARY KEY,
  domain_id         BIGINT       NOT NULL,
  app_type          VARCHAR(24)  NOT NULL,   -- 'wordpress' | 'nodejs' | 'php-composer'
  install_path      VARCHAR(500) NOT NULL,
  package_name      VARCHAR(200) NOT NULL,   -- plugin slug / npm pkg / composer pkg
  installed_version VARCHAR(64)  NOT NULL DEFAULT '',
  cve_id            VARCHAR(64)  NOT NULL,
  severity          VARCHAR(16)  NOT NULL DEFAULT '',  -- critical|high|medium|low|
  cvss              DECIMAL(3,1) NULL,
  title             TEXT         NULL,
  fixed_in          VARCHAR(64)  NULL,
  source            VARCHAR(64)  NOT NULL DEFAULT '',
  first_seen        DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  last_seen         DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE KEY uq_finding (domain_id, install_path, package_name, cve_id),
  KEY idx_severity (severity),
  KEY idx_domain (domain_id),
  KEY idx_app (app_type),
  KEY idx_last_seen (last_seen)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS cp_websec_status (
  id             INT          PRIMARY KEY,
  last_run       DATETIME     NULL,
  last_success   DATETIME     NULL,
  running        TINYINT(1)   NOT NULL DEFAULT 0,
  total_findings INT          NOT NULL DEFAULT 0,
  critical       INT          NOT NULL DEFAULT 0,
  high           INT          NOT NULL DEFAULT 0,
  medium         INT          NOT NULL DEFAULT 0,
  low            INT          NOT NULL DEFAULT 0,
  scanned_apps   INT          NOT NULL DEFAULT 0,
  duration_ms    INT          NOT NULL DEFAULT 0,
  last_error     TEXT         NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

INSERT IGNORE INTO cp_websec_status (id) VALUES (1);
