-- 0011 - service_plans kaynak limit kolonları (cgroups + xfs_quota + MySQL)
-- Idempotent: MariaDB 10.5+ ADD COLUMN IF NOT EXISTS destekler.

ALTER TABLE service_plans
  ADD COLUMN IF NOT EXISTS cpu_yuzde           INT NOT NULL DEFAULT 100,
  ADD COLUMN IF NOT EXISTS ram_mb              INT NOT NULL DEFAULT 512,
  ADD COLUMN IF NOT EXISTS max_process         INT NOT NULL DEFAULT 50,
  ADD COLUMN IF NOT EXISTS inode_kota          INT NOT NULL DEFAULT 50000,
  ADD COLUMN IF NOT EXISTS io_agirlik          INT NOT NULL DEFAULT 100,
  ADD COLUMN IF NOT EXISTS mysql_max_baglanti  INT NOT NULL DEFAULT 25;
