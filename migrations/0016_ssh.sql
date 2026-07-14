-- 0016 - host bazlı SSH erişimi (per-domain shell toggle)
ALTER TABLE domains
  ADD COLUMN IF NOT EXISTS ssh_erisim TINYINT(1) NOT NULL DEFAULT 0;
