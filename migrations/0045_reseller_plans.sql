-- 0045_reseller_plans.sql — Bayi (reseller) paketleri: adminin bayilere satacagi hazir limit paketleri.
-- Ornek: "Bronz Bayi: 10 hosting / 5 GB", "Gumus Bayi: 50 hosting / 50 GB".
-- Bayi olustururken paket secilir -> limitler users.max_* alanlarina kopyalanir (anlik-goruntu).
CREATE TABLE IF NOT EXISTS reseller_plans (
  id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  ad            VARCHAR(100) NOT NULL,
  aciklama      VARCHAR(255) NOT NULL DEFAULT '',
  max_domain    INT    NOT NULL DEFAULT 0,
  max_disk_mb   BIGINT NOT NULL DEFAULT 0,
  max_trafik_mb BIGINT NOT NULL DEFAULT 0,
  fiyat_kurus   BIGINT NOT NULL DEFAULT 0,
  varsayilan    TINYINT(1) NOT NULL DEFAULT 0,
  created_at    TIMESTAMP NULL DEFAULT current_timestamp(),
  PRIMARY KEY (id),
  UNIQUE KEY uq_rp_ad (ad)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

-- Bayinin hangi pakete bagli oldugu (0 = ozel/manuel limitler)
ALTER TABLE users ADD COLUMN IF NOT EXISTS reseller_plan_id BIGINT NOT NULL DEFAULT 0;
